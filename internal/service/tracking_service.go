package service

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"

	"ridematch-backend/internal/dto"
	"ridematch-backend/internal/models"
	"ridematch-backend/internal/repository"
	"ridematch-backend/internal/tracking"
)

const (
	// shiftLateFlushGrace is how long after a shift ends the app may still
	// upload buffered points for it (a phone that was offline at logout).
	shiftLateFlushGrace = 30 * time.Minute
	maxFutureSkew       = 2 * time.Minute
	maxTrailPoints      = 20000
	maxSegmentMeters    = 2000.0 // longer jumps are gaps, not driven distance
	maxAccuracyMeters   = 100.0
)

// TrackingService records and serves driver GPS evidence: every driver
// shift (where work started, where it ended, every point between) as a
// tamper-evident hash chain, so an admin can later prove where a driver
// was and what route a trip took.
type TrackingService struct {
	repo    repository.TrackingRepository
	drivers repository.DriverRepository
	trips   repository.TripRepository
	audit   *AuditService

	secret     []byte
	staleAfter time.Duration
	retention  time.Duration
}

// NewTrackingService constructs a TrackingService.
func NewTrackingService(
	repo repository.TrackingRepository, drivers repository.DriverRepository, trips repository.TripRepository,
	audit *AuditService, hmacSecret string, staleAfter, retention time.Duration,
) *TrackingService {
	return &TrackingService{
		repo: repo, drivers: drivers, trips: trips, audit: audit,
		secret: []byte(hmacSecret), staleAfter: staleAfter, retention: retention,
	}
}

// StartShift opens a shift when a driver goes online. If the driver still
// has a live shift (the app restarted or re-sent "online" while already
// working) that shift is continued instead of fragmenting the evidence.
func (s *TrackingService) StartShift(ctx context.Context, profile *models.DriverProfile, lat, lng float64) (*models.DriverShift, error) {
	if cur, err := s.repo.FindActiveShift(ctx, profile.ID); err == nil {
		last := cur.StartedAt
		if cur.LastPointAt != nil {
			last = *cur.LastPointAt
		}
		if time.Since(last) < s.staleAfter {
			return cur, nil
		}
	}
	now := time.Now()
	sh := &models.DriverShift{
		DriverID: profile.ID, UserID: profile.UserID, VehicleType: profile.VehicleType,
		StartedAt: now, StartLat: tracking.Round7(lat), StartLng: tracking.Round7(lng),
		LastLat: tracking.Round7(lat), LastLng: tracking.Round7(lng),
		LastHash: tracking.GenesisHash,
	}
	if err := s.repo.StartShift(ctx, sh); err != nil {
		return nil, fmt.Errorf("service: failed to start shift: %w", err)
	}
	return sh, nil
}

// EndActiveShift closes the driver's active shift, if any.
func (s *TrackingService) EndActiveShift(ctx context.Context, driverID string, reason models.ShiftEndReason) {
	sh, err := s.repo.FindActiveShift(ctx, driverID)
	if err != nil {
		if !errors.Is(err, repository.ErrNotFound) {
			log.Printf("tracking: failed to look up active shift for %s: %v", driverID, err)
		}
		return
	}
	lat, lng := sh.LastLat, sh.LastLng
	if _, err := s.repo.EndShift(ctx, sh.ID, reason, time.Now(), &lat, &lng); err != nil {
		log.Printf("tracking: failed to end shift %s: %v", sh.ID, err)
	}
}

// ActiveShiftID returns the id of the driver's active shift ("" if none).
func (s *TrackingService) ActiveShiftID(ctx context.Context, driverID string) string {
	sh, err := s.repo.FindActiveShift(ctx, driverID)
	if err != nil {
		return ""
	}
	return sh.ID
}

// Ingest appends a batch of GPS samples to a shift, extending its hash chain.
func (s *TrackingService) Ingest(ctx context.Context, driverUserID string, req dto.TrackingBatchRequest) (*dto.TrackingBatchResponse, error) {
	profile, err := s.drivers.FindByUserID(ctx, driverUserID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, ErrDriverProfileNotFound
		}
		return nil, fmt.Errorf("service: failed to load driver profile: %w", err)
	}
	shift, err := s.repo.FindShift(ctx, req.ShiftID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, ErrShiftNotFound
		}
		return nil, err
	}
	if shift.DriverID != profile.ID {
		return nil, ErrShiftNotFound // never reveal another driver's shift exists
	}
	if shift.Status == models.ShiftEnded && shift.EndedAt != nil && time.Since(*shift.EndedAt) > shiftLateFlushGrace {
		return nil, ErrShiftClosed
	}

	// Validate and order the input before taking the row lock.
	type parsed struct {
		in dto.TrackingPointInput
		at time.Time
	}
	now := time.Now()
	items := make([]parsed, 0, len(req.Points))
	for _, p := range req.Points {
		at, err := time.Parse(time.RFC3339, p.RecordedAt)
		if err != nil {
			return nil, fmt.Errorf("%w: recorded_at must be RFC3339", ErrInvalidPoint)
		}
		if at.After(now.Add(maxFutureSkew)) {
			return nil, fmt.Errorf("%w: recorded_at is in the future", ErrInvalidPoint)
		}
		items = append(items, parsed{in: p, at: at.UTC()})
	}
	// Sort by the app's counter so the chain order is the recording order.
	for i := 1; i < len(items); i++ {
		for j := i; j > 0 && items[j].in.N < items[j-1].in.N; j-- {
			items[j], items[j-1] = items[j-1], items[j]
		}
	}

	accepted, ignored := 0, 0
	var lastClient int64
	updated, err := s.repo.AppendPoints(ctx, shift.ID, func(sh *models.DriverShift) ([]models.LocationPoint, error) {
		out := make([]models.LocationPoint, 0, len(items))
		for _, it := range items {
			if it.in.N <= sh.LastClientSeq {
				ignored++
				continue
			}
			lat, lng := tracking.Round7(it.in.Lat), tracking.Round7(it.in.Lng)
			seq := sh.LastSeq + 1
			hash := tracking.PointHash(s.secret, sh.LastHash, sh.ID, seq, lat, lng, it.at, it.in.IsMock)
			out = append(out, models.LocationPoint{
				ShiftID: sh.ID, Seq: seq, DriverID: sh.DriverID, Lat: lat, Lng: lng,
				AccuracyM: it.in.AccuracyM, SpeedMps: it.in.SpeedMps, Heading: it.in.Heading,
				IsMock: it.in.IsMock, RecordedAt: it.at, ReceivedAt: now, Hash: hash,
			})
			if it.in.AccuracyM <= 0 || it.in.AccuracyM <= maxAccuracyMeters {
				seg := tracking.HaversineMeters(sh.LastLat, sh.LastLng, lat, lng)
				if seg <= maxSegmentMeters {
					sh.DistanceMeters += seg
				}
				sh.LastLat, sh.LastLng = lat, lng
			}
			sh.LastSeq = seq
			sh.LastHash = hash
			sh.LastClientSeq = it.in.N
			sh.PointCount++
			if it.in.IsMock {
				sh.MockPointCount++
			}
			if sh.LastPointAt == nil || it.at.After(*sh.LastPointAt) {
				t := it.at
				sh.LastPointAt = &t
			}
			accepted++
		}
		return out, nil
	})
	if err != nil {
		return nil, fmt.Errorf("service: failed to store location points: %w", err)
	}
	lastClient = updated.LastClientSeq
	return &dto.TrackingBatchResponse{Accepted: accepted, Ignored: ignored, LastClientSeq: lastClient}, nil
}

// CloseStaleShifts ends shifts that stopped sending points (phone died,
// app killed). Run periodically.
func (s *TrackingService) CloseStaleShifts(ctx context.Context) {
	rows, err := s.repo.StaleActiveShifts(ctx, time.Now().Add(-s.staleAfter), 200)
	if err != nil {
		log.Printf("tracking: stale shift scan failed: %v", err)
		return
	}
	for _, sh := range rows {
		at := sh.StartedAt
		if sh.LastPointAt != nil {
			at = *sh.LastPointAt
		}
		lat, lng := sh.LastLat, sh.LastLng
		if _, err := s.repo.EndShift(ctx, sh.ID, models.ShiftEndStale, at, &lat, &lng); err != nil {
			log.Printf("tracking: failed to close stale shift %s: %v", sh.ID, err)
		}
	}
}

// Purge removes trails past the retention window.
func (s *TrackingService) Purge(ctx context.Context) {
	if s.retention <= 0 {
		return
	}
	n, err := s.repo.PurgeBefore(ctx, time.Now().Add(-s.retention))
	if err != nil {
		log.Printf("tracking: purge failed: %v", err)
		return
	}
	if n > 0 {
		log.Printf("tracking: purged %d point(s) past retention", n)
	}
}

// ---- admin ----

func (s *TrackingService) toShiftResponse(ctx context.Context, sh *models.DriverShift, cache map[string]*models.DriverProfile) dto.ShiftResponse {
	resp := dto.ShiftResponse{
		ID: sh.ID, DriverID: sh.DriverID, VehicleType: string(sh.VehicleType), Status: string(sh.Status),
		StartedAt: sh.StartedAt, EndedAt: sh.EndedAt, EndReason: sh.EndReason,
		StartLat: sh.StartLat, StartLng: sh.StartLng, EndLat: sh.EndLat, EndLng: sh.EndLng,
		PointCount: sh.PointCount, MockPointCount: sh.MockPointCount,
		DistanceKM: float64(int64(sh.DistanceMeters/10)) / 100, LastPointAt: sh.LastPointAt,
	}
	p, ok := cache[sh.DriverID]
	if !ok {
		p, _ = s.drivers.FindByID(ctx, sh.DriverID)
		cache[sh.DriverID] = p
	}
	if p != nil {
		resp.PlateNumber = p.PlateNumber
		if p.User != nil {
			resp.DriverName = p.User.Name
			resp.DriverPhone = p.User.Phone
		}
	}
	return resp
}

func toTrackPoints(pts []models.LocationPoint) []dto.TrackPoint {
	out := make([]dto.TrackPoint, len(pts))
	for i, p := range pts {
		out[i] = dto.TrackPoint{Seq: p.Seq, Lat: p.Lat, Lng: p.Lng, AccuracyM: p.AccuracyM, SpeedMps: p.SpeedMps,
			Heading: p.Heading, IsMock: p.IsMock, RecordedAt: p.RecordedAt.UTC(), Hash: p.Hash}
	}
	return out
}

// ListShifts returns shift summaries for the admin tracking page.
func (s *TrackingService) ListShifts(ctx context.Context, f repository.ShiftFilter, page, pageSize int) ([]dto.ShiftResponse, int64, error) {
	page, pageSize = normalizePage(page, pageSize)
	rows, total, err := s.repo.ListShifts(ctx, f, pageSize, (page-1)*pageSize)
	if err != nil {
		return nil, 0, err
	}
	cache := map[string]*models.DriverProfile{}
	out := make([]dto.ShiftResponse, len(rows))
	for i := range rows {
		out[i] = s.toShiftResponse(ctx, &rows[i], cache)
	}
	return out, total, nil
}

// VerifyShift recomputes a shift's hash chain from the stored points.
func (s *TrackingService) VerifyShift(ctx context.Context, sh *models.DriverShift) (dto.ChainVerification, error) {
	v := dto.ChainVerification{Valid: true}
	prev := tracking.GenesisHash
	expectSeq := int64(1)
	var after int64
	for {
		batch, err := s.repo.AllPointsBySeq(ctx, sh.ID, after, 2000)
		if err != nil {
			return v, err
		}
		if len(batch) == 0 {
			break
		}
		for _, p := range batch {
			v.PointsChecked++
			bad := func(msg string) {
				if v.Valid {
					seq := p.Seq
					v.Valid = false
					v.FirstBadSeq = &seq
					v.Problem = msg
				}
			}
			if p.Seq != expectSeq {
				bad("a point is missing from the sequence")
				expectSeq = p.Seq
			}
			want := tracking.PointHash(s.secret, prev, sh.ID, p.Seq, p.Lat, p.Lng, p.RecordedAt, p.IsMock)
			if want != p.Hash {
				bad("a point's contents do not match its signature")
			}
			prev = p.Hash
			expectSeq++
			after = p.Seq
		}
	}
	if v.Valid && v.PointsChecked != sh.PointCount {
		v.Valid = false
		v.Problem = fmt.Sprintf("expected %d points, found %d", sh.PointCount, v.PointsChecked)
	}
	if v.Valid && sh.LastHash != "" && prev != sh.LastHash && v.PointsChecked > 0 {
		v.Valid = false
		v.Problem = "the chain head does not match the shift record"
	}
	return v, nil
}

// ShiftDetail returns a shift, its trail and integrity check. Viewing is audited.
func (s *TrackingService) ShiftDetail(ctx context.Context, actor Actor, id string) (*dto.ShiftDetailResponse, error) {
	sh, err := s.repo.FindShift(ctx, id)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, ErrShiftNotFound
		}
		return nil, err
	}
	pts, err := s.repo.ListPoints(ctx, id, maxTrailPoints+1)
	if err != nil {
		return nil, err
	}
	truncated := len(pts) > maxTrailPoints
	if truncated {
		pts = pts[:maxTrailPoints]
	}
	ver, err := s.VerifyShift(ctx, sh)
	if err != nil {
		return nil, err
	}
	s.audit.Record(ctx, actor, "tracking.view_shift", "shift", id, nil)
	return &dto.ShiftDetailResponse{
		Shift: s.toShiftResponse(ctx, sh, map[string]*models.DriverProfile{}), Points: toTrackPoints(pts),
		Truncated: truncated, Verification: ver,
	}, nil
}

// TripRoute returns the recorded route for a trip: the assigned driver's
// trail from the moment of match until the trip ended. Viewing is audited.
func (s *TrackingService) TripRoute(ctx context.Context, actor Actor, tripID string) (*dto.TripRouteResponse, error) {
	trip, err := s.trips.FindByID(ctx, tripID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, ErrTripNotFound
		}
		return nil, err
	}
	resp := &dto.TripRouteResponse{
		TripID: trip.ID, Status: string(trip.Status), PickupLat: trip.PickupLat, PickupLng: trip.PickupLng,
		DestLat: trip.DestinationLat, DestLng: trip.DestinationLng,
		MatchedAt: trip.MatchedAt, PickedUpAt: trip.PickupConfirmedAt, Points: []dto.TrackPoint{},
	}
	if trip.DriverID == nil {
		return resp, nil
	}
	resp.DriverID = *trip.DriverID
	if p, err := s.drivers.FindByID(ctx, *trip.DriverID); err == nil && p.User != nil {
		resp.DriverName = p.User.Name
	}
	start := trip.CreatedAt
	if trip.MatchedAt != nil {
		start = *trip.MatchedAt
	}
	end := time.Now()
	switch {
	case trip.CompletedAt != nil:
		end = *trip.CompletedAt
	case trip.CancelledAt != nil:
		end = *trip.CancelledAt
	}
	resp.EndedAt = trip.CompletedAt
	if resp.EndedAt == nil {
		resp.EndedAt = trip.CancelledAt
	}
	pts, err := s.repo.PointsInWindow(ctx, *trip.DriverID, start.Add(-30*time.Second), end.Add(30*time.Second), maxTrailPoints)
	if err != nil {
		return nil, err
	}
	resp.Points = toTrackPoints(pts)
	for i := 1; i < len(pts); i++ {
		if seg := tracking.HaversineMeters(pts[i-1].Lat, pts[i-1].Lng, pts[i].Lat, pts[i].Lng); seg <= maxSegmentMeters {
			resp.DistanceKM += seg / 1000
		}
	}
	resp.DistanceKM = float64(int64(resp.DistanceKM*100)) / 100
	s.audit.Record(ctx, actor, "tracking.view_trip_route", "trip", tripID, nil)
	return resp, nil
}

// ExportShift renders a shift's evidence as geojson, csv or json. The
// export is audited and embeds the integrity verdict.
func (s *TrackingService) ExportShift(ctx context.Context, actor Actor, id, format string) (contentType, filename string, body []byte, err error) {
	sh, err := s.repo.FindShift(ctx, id)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return "", "", nil, ErrShiftNotFound
		}
		return "", "", nil, err
	}
	pts, err := s.repo.ListPoints(ctx, id, 200000)
	if err != nil {
		return "", "", nil, err
	}
	ver, err := s.VerifyShift(ctx, sh)
	if err != nil {
		return "", "", nil, err
	}
	summary := s.toShiftResponse(ctx, sh, map[string]*models.DriverProfile{})
	s.audit.Record(ctx, actor, "tracking.export_shift", "shift", id, map[string]interface{}{"format": format})

	switch strings.ToLower(format) {
	case "csv":
		var b strings.Builder
		w := csv.NewWriter(&b)
		_ = w.Write([]string{"seq", "recorded_at_utc", "lat", "lng", "accuracy_m", "speed_mps", "heading", "is_mock", "hash"})
		for _, p := range pts {
			_ = w.Write([]string{
				strconv.FormatInt(p.Seq, 10), p.RecordedAt.UTC().Format(time.RFC3339Nano),
				strconv.FormatFloat(p.Lat, 'f', 7, 64), strconv.FormatFloat(p.Lng, 'f', 7, 64),
				strconv.FormatFloat(float64(p.AccuracyM), 'f', 1, 64), strconv.FormatFloat(float64(p.SpeedMps), 'f', 2, 64),
				strconv.FormatFloat(float64(p.Heading), 'f', 0, 64), strconv.FormatBool(p.IsMock), p.Hash,
			})
		}
		w.Flush()
		return "text/csv", "shift-" + id + ".csv", []byte(b.String()), nil
	case "geojson":
		coords := make([][]float64, len(pts))
		for i, p := range pts {
			coords[i] = []float64{p.Lng, p.Lat}
		}
		fc := map[string]interface{}{
			"type": "Feature",
			"properties": map[string]interface{}{
				"shift": summary, "integrity": ver, "exported_at": time.Now().UTC(),
			},
			"geometry": map[string]interface{}{"type": "LineString", "coordinates": coords},
		}
		b, _ := json.MarshalIndent(fc, "", "  ")
		return "application/geo+json", "shift-" + id + ".geojson", b, nil
	default:
		b, _ := json.MarshalIndent(map[string]interface{}{
			"shift": summary, "integrity": ver, "points": toTrackPoints(pts), "exported_at": time.Now().UTC(),
		}, "", "  ")
		return "application/json", "shift-" + id + ".json", b, nil
	}
}
