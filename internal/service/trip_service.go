package service

import (
	"context"
	"errors"
	"fmt"
	"math"
	"time"

	"ridematch-backend/internal/dto"
	"ridematch-backend/internal/models"
	"ridematch-backend/internal/repository"
	"ridematch-backend/internal/storage"
	"ridematch-backend/internal/utils"
	"ridematch-backend/internal/ws"
)

// pickupPINLength is short by design — it's read aloud/shown in person
// between two people who are already physically together, not typed
// under pressure like a bank OTP.
const pickupPINLength = 4

// TripService implements the full trip lifecycle: a passenger requests a
// ride, nearby drivers are notified and can make price offers, the
// passenger accepts one (generating a pickup PIN), the driver confirms
// pickup with that PIN, and finally marks the trip complete. Every state
// transition is authorization-checked against the caller and applied with
// an atomic compare-and-swap against the trip's current status, so two
// concurrent requests (a double-tapped button, a retry after a slow
// network) can never both succeed and corrupt the trip's state.
type TripService struct {
	trips           repository.TripRepository
	offers          repository.TripOfferRepository
	drivers         repository.DriverRepository
	users           repository.UserRepository
	ratings         repository.TripRatingRepository
	tripLocations   repository.TripLocationRepository
	driverLocations repository.LocationRepository
	hub             *ws.Hub
	fileStore       storage.Store

	expiryAfter         time.Duration
	nearbyRadiusKM      float64
	nearbyLimit         int
	coordinatePrecision int

	// locationStaleAfter mirrors DriverService's staleness window — a
	// driver's last location ping is trusted for this long before they're
	// treated as offline. Must be a real (non-zero) window: passing 0
	// here would set the freshness cutoff to "now", which discards every
	// driver (even one that pinged a millisecond ago) as stale and, via
	// FindNearby's lazy-eviction side effect, would incorrectly drop them
	// from the geo index entirely.
	locationStaleAfter time.Duration
}

// NewTripService constructs a TripService from its dependencies.
func NewTripService(
	trips repository.TripRepository,
	offers repository.TripOfferRepository,
	drivers repository.DriverRepository,
	users repository.UserRepository,
	ratings repository.TripRatingRepository,
	tripLocations repository.TripLocationRepository,
	driverLocations repository.LocationRepository,
	hub *ws.Hub,
	fileStore storage.Store,
	expiryAfter time.Duration,
	nearbyRadiusKM float64,
	nearbyLimit int,
	coordinatePrecision int,
	locationStaleAfter time.Duration,
) *TripService {
	return &TripService{
		trips:               trips,
		offers:              offers,
		drivers:             drivers,
		users:               users,
		ratings:             ratings,
		tripLocations:       tripLocations,
		driverLocations:     driverLocations,
		hub:                 hub,
		fileStore:           fileStore,
		expiryAfter:         expiryAfter,
		nearbyRadiusKM:      nearbyRadiusKM,
		nearbyLimit:         nearbyLimit,
		coordinatePrecision: coordinatePrecision,
		locationStaleAfter:  locationStaleAfter,
	}
}

// CreateTrip opens a new trip request, indexes it for nearby-driver
// discovery, and pushes an instant WebSocket notification to every
// currently online, connected driver of the right vehicle type nearby.
// Drivers who aren't connected (or missed the push) still find it via
// GET /trips/nearby, which reads the same geo index.
func (s *TripService) CreateTrip(ctx context.Context, passengerUserID string, req dto.CreateTripRequest) (*dto.TripResponse, error) {
	if _, err := s.trips.FindActiveByPassenger(ctx, passengerUserID); err == nil {
		return nil, ErrTripAlreadyActive
	} else if !errors.Is(err, repository.ErrNotFound) {
		return nil, fmt.Errorf("service: failed to check active trip: %w", err)
	}

	trip := &models.Trip{
		PassengerID:        passengerUserID,
		VehicleType:        models.VehicleType(req.VehicleType),
		Status:             models.TripRequested,
		PickupLat:          req.PickupLat,
		PickupLng:          req.PickupLng,
		PickupAddress:      req.PickupAddress,
		DestinationLat:     req.DestinationLat,
		DestinationLng:     req.DestinationLng,
		DestinationAddress: req.DestinationAddress,
		OfferedPriceKobo:   req.OfferedPriceKobo,
	}
	if err := s.trips.Create(ctx, trip); err != nil {
		return nil, fmt.Errorf("service: failed to create trip: %w", err)
	}

	if err := s.tripLocations.AddOpenTrip(ctx, trip.ID, trip.VehicleType, trip.PickupLat, trip.PickupLng); err != nil {
		// The trip record itself is safely created; a failed geo-index
		// write just means this trip won't surface in nearby searches
		// until the driver polls a wider net or the failure is
		// retried — not a reason to fail the whole request.
		_ = err
	} else {
		s.notifyNearbyDrivers(ctx, trip)
	}

	resp := s.toTripResponse(ctx, trip)
	return &resp, nil
}

// notifyNearbyDrivers pushes a WebSocket event to every online driver of
// the matching vehicle type within the configured radius. Best-effort:
// drivers not connected simply rely on polling GET /trips/nearby instead.
func (s *TripService) notifyNearbyDrivers(ctx context.Context, trip *models.Trip) {
	nearby, err := s.driverLocations.FindNearby(ctx, trip.VehicleType, trip.PickupLat, trip.PickupLng, s.nearbyRadiusKM, s.nearbyLimit, s.locationStaleAfter)
	if err != nil {
		return
	}

	payload := dto.NearbyOpenTripResponse{
		TripID:           trip.ID,
		VehicleType:      string(trip.VehicleType),
		PickupLat:        roundTo(trip.PickupLat, s.coordinatePrecision),
		PickupLng:        roundTo(trip.PickupLng, s.coordinatePrecision),
		OfferedPriceKobo: trip.OfferedPriceKobo,
	}

	for _, n := range nearby {
		profile, err := s.drivers.FindByID(ctx, n.DriverID)
		if err != nil {
			continue
		}
		payload.DistanceKM = roundTo(n.DistanceKM, 2)
		s.hub.SendToUser(profile.UserID, ws.Event{Type: ws.EventNewTripRequest, Data: payload})
	}
}

// GetTrip returns a trip to its passenger or matched driver. Lazily
// expires an open trip whose request window has elapsed rather than
// requiring a background sweeper — the next read or write against it
// self-heals the state.
func (s *TripService) GetTrip(ctx context.Context, callerUserID, tripID string) (*dto.TripResponse, error) {
	trip, err := s.loadAndExpireIfDue(ctx, tripID)
	if err != nil {
		return nil, err
	}
	if err := s.assertParticipant(trip, callerUserID); err != nil {
		return nil, err
	}
	resp := s.toTripResponse(ctx, trip)
	return &resp, nil
}

// MyActiveTrip returns the caller's current in-progress trip, as a
// passenger. Returns ErrTripNotFound if they have none.
func (s *TripService) MyActiveTrip(ctx context.Context, passengerUserID string) (*dto.TripResponse, error) {
	trip, err := s.trips.FindActiveByPassenger(ctx, passengerUserID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, ErrTripNotFound
		}
		return nil, fmt.Errorf("service: failed to load active trip: %w", err)
	}
	resp := s.toTripResponse(ctx, trip)
	return &resp, nil
}

// MyActiveTripAsDriver returns the caller's current in-progress trip, as
// the matched driver (status matched or picked_up) — symmetric to
// MyActiveTrip, which is the passenger's view. Needed so the app can
// rehydrate "you have a trip in progress" for a driver on cold start,
// not just from the WebSocket event that first announced the match.
func (s *TripService) MyActiveTripAsDriver(ctx context.Context, driverUserID string) (*dto.TripResponse, error) {
	profile, err := s.drivers.FindByUserID(ctx, driverUserID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, ErrDriverProfileNotFound
		}
		return nil, fmt.Errorf("service: failed to load driver profile: %w", err)
	}

	trip, err := s.trips.FindActiveByDriver(ctx, profile.ID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, ErrTripNotFound
		}
		return nil, fmt.Errorf("service: failed to load active trip: %w", err)
	}
	resp := s.toTripResponse(ctx, trip)
	return &resp, nil
}

// defaultHistoryPageSize and maxHistoryPageSize bound page_size on the
// history endpoint — unset or invalid values fall back to the default,
// and anything above the max is clamped rather than rejected.
const (
	defaultHistoryPageSize = 20
	maxHistoryPageSize     = 50
)

// History returns a page of the passenger's own trips, newest first,
// across every status.
func (s *TripService) History(ctx context.Context, passengerUserID string, page, pageSize int) (*dto.TripHistoryResponse, error) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = defaultHistoryPageSize
	}
	if pageSize > maxHistoryPageSize {
		pageSize = maxHistoryPageSize
	}

	trips, total, err := s.trips.FindHistoryByPassenger(ctx, passengerUserID, pageSize, (page-1)*pageSize)
	if err != nil {
		return nil, fmt.Errorf("service: failed to load trip history: %w", err)
	}

	resp := make([]dto.TripResponse, len(trips))
	for i := range trips {
		resp[i] = s.toTripResponse(ctx, &trips[i])
	}

	return &dto.TripHistoryResponse{Trips: resp, Page: page, PageSize: pageSize, Total: total}, nil
}

// DriverHistory returns a page of the calling driver's own trips (every
// status, newest first) — symmetric to History, the passenger equivalent.
func (s *TripService) DriverHistory(ctx context.Context, driverUserID string, page, pageSize int) (*dto.TripHistoryResponse, error) {
	profile, err := s.drivers.FindByUserID(ctx, driverUserID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, ErrDriverProfileNotFound
		}
		return nil, fmt.Errorf("service: failed to load driver profile: %w", err)
	}

	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = defaultHistoryPageSize
	}
	if pageSize > maxHistoryPageSize {
		pageSize = maxHistoryPageSize
	}

	trips, total, err := s.trips.FindHistoryByDriver(ctx, profile.ID, pageSize, (page-1)*pageSize)
	if err != nil {
		return nil, fmt.Errorf("service: failed to load driver trip history: %w", err)
	}

	resp := make([]dto.TripResponse, len(trips))
	for i := range trips {
		resp[i] = s.toTripResponse(ctx, &trips[i])
	}

	return &dto.TripHistoryResponse{Trips: resp, Page: page, PageSize: pageSize, Total: total}, nil
}

// DriverEarnings returns the calling driver's real, SQL-aggregated
// earnings summary plus a short recent-trips list for context.
func (s *TripService) DriverEarnings(ctx context.Context, driverUserID string) (*dto.DriverEarningsResponse, error) {
	profile, err := s.drivers.FindByUserID(ctx, driverUserID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, ErrDriverProfileNotFound
		}
		return nil, fmt.Errorf("service: failed to load driver profile: %w", err)
	}

	totalKobo, totalTrips, todayKobo, todayTrips, err := s.trips.SumEarningsByDriver(ctx, profile.ID)
	if err != nil {
		return nil, fmt.Errorf("service: failed to sum driver earnings: %w", err)
	}

	const recentLimit = 10
	recent, _, err := s.trips.FindHistoryByDriver(ctx, profile.ID, recentLimit, 0)
	if err != nil {
		return nil, fmt.Errorf("service: failed to load recent driver trips: %w", err)
	}
	recentResp := make([]dto.TripResponse, len(recent))
	for i := range recent {
		recentResp[i] = s.toTripResponse(ctx, &recent[i])
	}

	return &dto.DriverEarningsResponse{
		TotalEarnedKobo: totalKobo,
		TotalTripsDone:  totalTrips,
		TodayEarnedKobo: todayKobo,
		TodayTripsDone:  todayTrips,
		RecentTrips:     recentResp,
	}, nil
}

// FindNearbyOpenTrips is the REST fallback for drivers who aren't
// WebSocket-connected (or want to actively browse): open trip requests
// near a point, for their vehicle type.
func (s *TripService) FindNearbyOpenTrips(ctx context.Context, vehicleType string, lat, lng, radiusKM float64, limit int) ([]dto.NearbyOpenTripResponse, error) {
	if radiusKM <= 0 {
		radiusKM = s.nearbyRadiusKM
	}
	if limit <= 0 {
		limit = s.nearbyLimit
	}

	nearby, err := s.tripLocations.FindNearbyOpenTrips(ctx, models.VehicleType(vehicleType), lat, lng, radiusKM, limit)
	if err != nil {
		return nil, err
	}

	results := make([]dto.NearbyOpenTripResponse, 0, len(nearby))
	for _, n := range nearby {
		trip, err := s.trips.FindByID(ctx, n.TripID)
		if err != nil || !trip.IsOpen() {
			continue
		}
		results = append(results, dto.NearbyOpenTripResponse{
			TripID:           trip.ID,
			VehicleType:      string(trip.VehicleType),
			PickupLat:        roundTo(n.Latitude, s.coordinatePrecision),
			PickupLng:        roundTo(n.Longitude, s.coordinatePrecision),
			DistanceKM:       roundTo(n.DistanceKM, 2),
			OfferedPriceKobo: trip.OfferedPriceKobo,
		})
	}
	return results, nil
}

// MakeOffer records (or updates) a driver's price offer on an open trip
// and pushes it to the passenger instantly.
func (s *TripService) MakeOffer(ctx context.Context, driverUserID, tripID string, priceKobo int64) (*dto.OfferResponse, error) {
	profile, err := s.requireActiveDriver(ctx, driverUserID)
	if err != nil {
		return nil, err
	}

	trip, err := s.loadAndExpireIfDue(ctx, tripID)
	if err != nil {
		return nil, err
	}
	if !trip.IsOpen() {
		return nil, ErrTripNotOpen
	}
	if trip.VehicleType != profile.VehicleType {
		return nil, ErrTripNotOpen
	}

	existing, err := s.offers.FindPendingByTripAndDriver(ctx, tripID, profile.ID)
	var offer *models.TripOffer
	switch {
	case err == nil:
		existing.PriceKobo = priceKobo
		if err := s.offers.Update(ctx, existing); err != nil {
			return nil, fmt.Errorf("service: failed to update offer: %w", err)
		}
		offer = existing
	case errors.Is(err, repository.ErrNotFound):
		offer = &models.TripOffer{TripID: tripID, DriverID: profile.ID, PriceKobo: priceKobo, Status: models.OfferPending}
		if err := s.offers.Create(ctx, offer); err != nil {
			return nil, fmt.Errorf("service: failed to create offer: %w", err)
		}
	default:
		return nil, fmt.Errorf("service: failed to check existing offer: %w", err)
	}

	resp := s.toOfferResponse(ctx, offer, profile)
	s.hub.SendToUser(trip.PassengerID, ws.Event{Type: ws.EventOfferReceived, Data: resp})

	return &resp, nil
}

// ListOffers returns every currently pending offer on a trip, for its
// passenger to review.
func (s *TripService) ListOffers(ctx context.Context, passengerUserID, tripID string) ([]dto.OfferResponse, error) {
	trip, err := s.trips.FindByID(ctx, tripID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, ErrTripNotFound
		}
		return nil, fmt.Errorf("service: failed to load trip: %w", err)
	}
	if trip.PassengerID != passengerUserID {
		return nil, ErrNotTripOwner
	}

	offers, err := s.offers.FindPendingByTrip(ctx, tripID)
	if err != nil {
		return nil, fmt.Errorf("service: failed to load offers: %w", err)
	}

	results := make([]dto.OfferResponse, len(offers))
	for i, o := range offers {
		results[i] = s.toOfferResponse(ctx, &o, o.Driver)
	}
	return results, nil
}

// AcceptOffer matches the trip to the offering driver: atomically flips
// the trip to "matched" (failing safely if it was already matched or
// closed by a concurrent request), generates the one-time pickup PIN, and
// notifies the driver and every other offering driver in real time.
func (s *TripService) AcceptOffer(ctx context.Context, passengerUserID, tripID, offerID string) (*dto.AcceptOfferResponse, error) {
	trip, err := s.trips.FindByID(ctx, tripID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, ErrTripNotFound
		}
		return nil, fmt.Errorf("service: failed to load trip: %w", err)
	}
	if trip.PassengerID != passengerUserID {
		return nil, ErrNotTripOwner
	}
	if !trip.IsOpen() {
		return nil, ErrTripNotOpen
	}

	offer, err := s.offers.FindByID(ctx, offerID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, ErrOfferNotFound
		}
		return nil, fmt.Errorf("service: failed to load offer: %w", err)
	}
	if offer.TripID != tripID {
		return nil, ErrOfferNotFound
	}
	if offer.Status != models.OfferPending {
		return nil, ErrOfferNotPending
	}

	// Re-verify the driver is still eligible at the moment of matching —
	// their subscription or verification status may have changed since
	// they placed the offer.
	profile := offer.Driver
	if profile == nil {
		profile, err = s.drivers.FindByID(ctx, offer.DriverID)
		if err != nil {
			return nil, fmt.Errorf("service: failed to load offering driver: %w", err)
		}
	}
	if !profile.IsApproved() || !profile.HasActiveSubscription() {
		return nil, ErrSubscriptionInactive
	}
	if _, err := s.trips.FindActiveByDriver(ctx, profile.ID); err == nil {
		return nil, ErrTripAlreadyActive
	} else if !errors.Is(err, repository.ErrNotFound) {
		return nil, fmt.Errorf("service: failed to check driver availability: %w", err)
	}

	// Claim the offer first (cheap, single-row CAS): if this fails,
	// someone else (or a duplicate/racing request) already resolved it.
	claimed, err := s.offers.CompareAndSwapStatus(ctx, offer.ID, models.OfferPending, models.OfferAccepted)
	if err != nil {
		return nil, fmt.Errorf("service: failed to claim offer: %w", err)
	}
	if !claimed {
		return nil, ErrOfferNotPending
	}

	pin, pinHash, err := generatePickupPIN()
	if err != nil {
		return nil, err
	}

	matched, err := s.trips.CompareAndSwapStatus(ctx, tripID, models.TripRequested, map[string]interface{}{
		"status":            models.TripMatched,
		"driver_id":         profile.ID,
		"agreed_price_kobo": offer.PriceKobo,
		"pickup_pin_hash":   pinHash,
	})
	if err != nil {
		return nil, fmt.Errorf("service: failed to match trip: %w", err)
	}
	if !matched {
		// The offer claim succeeded but the trip itself was closed by a
		// concurrent request (e.g. the passenger cancelled at the same
		// instant). Roll the offer claim back so it isn't left stranded
		// as "accepted" on a trip that never actually matched.
		_, _ = s.offers.CompareAndSwapStatus(ctx, offer.ID, models.OfferAccepted, models.OfferPending)
		return nil, ErrTripNotOpen
	}

	pendingDriverIDs, _ := s.offers.PendingDriverIDs(ctx, tripID)
	_ = s.offers.SupersedeOtherPending(ctx, tripID, offer.ID)
	_ = s.tripLocations.RemoveOpenTrip(ctx, tripID, trip.VehicleType)

	trip, err = s.trips.FindByID(ctx, tripID)
	if err != nil {
		return nil, fmt.Errorf("service: failed to reload matched trip: %w", err)
	}

	resp := s.toTripResponse(ctx, trip)
	s.hub.SendToUser(trip.PassengerID, ws.Event{Type: ws.EventTripMatched, Data: resp})
	s.hub.SendToUser(profile.UserID, ws.Event{Type: ws.EventTripMatched, Data: resp})

	for _, driverID := range pendingDriverIDs {
		if driverID == profile.ID {
			continue
		}
		if other, err := s.drivers.FindByID(ctx, driverID); err == nil {
			s.hub.SendToUser(other.UserID, ws.Event{Type: ws.EventTripClosed, Data: map[string]string{"trip_id": tripID}})
		}
	}

	return &dto.AcceptOfferResponse{Trip: resp, PickupPIN: pin}, nil
}

// RejectOffer declines a driver's offer, freeing them to see the trip
// remains open (if it does) rather than waiting indefinitely.
func (s *TripService) RejectOffer(ctx context.Context, passengerUserID, tripID, offerID string) error {
	trip, err := s.trips.FindByID(ctx, tripID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return ErrTripNotFound
		}
		return fmt.Errorf("service: failed to load trip: %w", err)
	}
	if trip.PassengerID != passengerUserID {
		return ErrNotTripOwner
	}

	offer, err := s.offers.FindByID(ctx, offerID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return ErrOfferNotFound
		}
		return fmt.Errorf("service: failed to load offer: %w", err)
	}
	if offer.TripID != tripID {
		return ErrOfferNotFound
	}

	rejected, err := s.offers.CompareAndSwapStatus(ctx, offer.ID, models.OfferPending, models.OfferRejected)
	if err != nil {
		return fmt.Errorf("service: failed to reject offer: %w", err)
	}
	if !rejected {
		return ErrOfferNotPending
	}

	profile := offer.Driver
	if profile == nil {
		profile, err = s.drivers.FindByID(ctx, offer.DriverID)
	}
	if err == nil && profile != nil {
		s.hub.SendToUser(profile.UserID, ws.Event{Type: ws.EventOfferRejected, Data: map[string]string{"trip_id": tripID, "offer_id": offerID}})
	}
	return nil
}

// ConfirmPickup is the feature that started this whole build: proof a
// driver actually picked up the passenger they matched with, not just a
// self-reported status flip. The passenger shows the driver the PIN in
// person; the driver enters it here.
func (s *TripService) ConfirmPickup(ctx context.Context, driverUserID, tripID, pin string) (*dto.TripResponse, error) {
	profile, err := s.drivers.FindByUserID(ctx, driverUserID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, ErrDriverProfileNotFound
		}
		return nil, fmt.Errorf("service: failed to load driver profile: %w", err)
	}

	trip, err := s.trips.FindByID(ctx, tripID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, ErrTripNotFound
		}
		return nil, fmt.Errorf("service: failed to load trip: %w", err)
	}
	if trip.DriverID == nil || *trip.DriverID != profile.ID {
		return nil, ErrNotMatchedDriver
	}
	if trip.Status != models.TripMatched {
		return nil, ErrTripNotMatched
	}
	if !utils.CompareOTP(trip.PickupPINHash, pin) {
		return nil, ErrInvalidPickupPIN
	}

	confirmed, err := s.trips.CompareAndSwapStatus(ctx, tripID, models.TripMatched, map[string]interface{}{
		"status":              models.TripPickedUp,
		"pickup_confirmed_at": time.Now(),
	})
	if err != nil {
		return nil, fmt.Errorf("service: failed to confirm pickup: %w", err)
	}
	if !confirmed {
		return nil, ErrTripNotMatched
	}

	trip, err = s.trips.FindByID(ctx, tripID)
	if err != nil {
		return nil, fmt.Errorf("service: failed to reload trip: %w", err)
	}

	resp := s.toTripResponse(ctx, trip)
	s.hub.SendToUser(trip.PassengerID, ws.Event{Type: ws.EventTripPickedUp, Data: resp})
	return &resp, nil
}

// CompleteTrip closes out a picked-up trip.
func (s *TripService) CompleteTrip(ctx context.Context, driverUserID, tripID string) (*dto.TripResponse, error) {
	profile, err := s.drivers.FindByUserID(ctx, driverUserID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, ErrDriverProfileNotFound
		}
		return nil, fmt.Errorf("service: failed to load driver profile: %w", err)
	}

	trip, err := s.trips.FindByID(ctx, tripID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, ErrTripNotFound
		}
		return nil, fmt.Errorf("service: failed to load trip: %w", err)
	}
	if trip.DriverID == nil || *trip.DriverID != profile.ID {
		return nil, ErrNotMatchedDriver
	}
	if trip.Status != models.TripPickedUp {
		return nil, ErrTripNotPickedUp
	}

	completed, err := s.trips.CompareAndSwapStatus(ctx, tripID, models.TripPickedUp, map[string]interface{}{
		"status":       models.TripCompleted,
		"completed_at": time.Now(),
	})
	if err != nil {
		return nil, fmt.Errorf("service: failed to complete trip: %w", err)
	}
	if !completed {
		return nil, ErrTripNotPickedUp
	}

	trip, err = s.trips.FindByID(ctx, tripID)
	if err != nil {
		return nil, fmt.Errorf("service: failed to reload trip: %w", err)
	}

	resp := s.toTripResponse(ctx, trip)
	s.hub.SendToUser(trip.PassengerID, ws.Event{Type: ws.EventTripCompleted, Data: resp})
	return &resp, nil
}

// CancelTrip ends a trip before pickup, from either side. An open request
// with pending offers notifies each offering driver; a matched trip
// notifies the other party.
func (s *TripService) CancelTrip(ctx context.Context, callerUserID, tripID, reason string) (*dto.TripResponse, error) {
	trip, err := s.trips.FindByID(ctx, tripID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, ErrTripNotFound
		}
		return nil, fmt.Errorf("service: failed to load trip: %w", err)
	}

	var cancelledBy models.CancelledBy
	switch {
	case trip.PassengerID == callerUserID:
		cancelledBy = models.CancelledByPassenger
	case trip.Driver != nil && trip.Driver.UserID == callerUserID:
		cancelledBy = models.CancelledByDriver
	default:
		// Driver may not be preloaded depending on the caller path;
		// resolve it directly before giving up.
		if profile, err := s.drivers.FindByUserID(ctx, callerUserID); err == nil && trip.DriverID != nil && *trip.DriverID == profile.ID {
			cancelledBy = models.CancelledByDriver
		} else {
			return nil, ErrNotTripOwner
		}
	}

	if trip.Status != models.TripRequested && trip.Status != models.TripMatched {
		return nil, ErrTripNotCancelable
	}

	cancelled, err := s.trips.CompareAndSwapStatus(ctx, tripID, trip.Status, map[string]interface{}{
		"status":        models.TripCancelled,
		"cancelled_at":  time.Now(),
		"cancelled_by":  cancelledBy,
		"cancel_reason": reason,
	})
	if err != nil {
		return nil, fmt.Errorf("service: failed to cancel trip: %w", err)
	}
	if !cancelled {
		return nil, ErrTripNotCancelable
	}

	_ = s.tripLocations.RemoveOpenTrip(ctx, tripID, trip.VehicleType)

	if trip.Status == models.TripRequested {
		pendingDriverIDs, _ := s.offers.PendingDriverIDs(ctx, tripID)
		_ = s.offers.WithdrawAllPending(ctx, tripID)
		for _, driverID := range pendingDriverIDs {
			if other, err := s.drivers.FindByID(ctx, driverID); err == nil {
				s.hub.SendToUser(other.UserID, ws.Event{Type: ws.EventTripClosed, Data: map[string]string{"trip_id": tripID}})
			}
		}
	}

	trip, err = s.trips.FindByID(ctx, tripID)
	if err != nil {
		return nil, fmt.Errorf("service: failed to reload trip: %w", err)
	}
	resp := s.toTripResponse(ctx, trip)

	// Notify whichever side didn't do the cancelling. A driver can only
	// ever cancel a trip that was already matched to them (the
	// authorization check above guarantees that), so the only case with
	// no one to notify is a passenger cancelling before any driver was
	// matched — trip.Driver is nil precisely then.
	switch {
	case cancelledBy == models.CancelledByDriver:
		s.hub.SendToUser(trip.PassengerID, ws.Event{Type: ws.EventTripCancelled, Data: resp})
	case cancelledBy == models.CancelledByPassenger && trip.Driver != nil:
		s.hub.SendToUser(trip.Driver.UserID, ws.Event{Type: ws.EventTripCancelled, Data: resp})
	}

	return &resp, nil
}

// loadAndExpireIfDue fetches a trip and, if it's still "requested" but
// has been open longer than the configured expiry window, atomically
// expires it (and cleans up its geo index entry and any pending offers)
// before returning — so an abandoned request doesn't sit visible to
// drivers forever.
func (s *TripService) loadAndExpireIfDue(ctx context.Context, tripID string) (*models.Trip, error) {
	trip, err := s.trips.FindByID(ctx, tripID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, ErrTripNotFound
		}
		return nil, fmt.Errorf("service: failed to load trip: %w", err)
	}

	if trip.Status == models.TripRequested && time.Since(trip.CreatedAt) > s.expiryAfter {
		expired, err := s.trips.CompareAndSwapStatus(ctx, tripID, models.TripRequested, map[string]interface{}{
			"status": models.TripExpired,
		})
		if err == nil && expired {
			_ = s.tripLocations.RemoveOpenTrip(ctx, tripID, trip.VehicleType)
			pendingDriverIDs, _ := s.offers.PendingDriverIDs(ctx, tripID)
			_ = s.offers.WithdrawAllPending(ctx, tripID)
			for _, driverID := range pendingDriverIDs {
				if other, err := s.drivers.FindByID(ctx, driverID); err == nil {
					s.hub.SendToUser(other.UserID, ws.Event{Type: ws.EventTripClosed, Data: map[string]string{"trip_id": tripID}})
				}
			}
			trip.Status = models.TripExpired
		}
	}

	return trip, nil
}

// requireActiveDriver loads a driver's profile and confirms they're
// eligible to act (approved, subscribed, online) before letting them
// touch a trip.
func (s *TripService) requireActiveDriver(ctx context.Context, driverUserID string) (*models.DriverProfile, error) {
	profile, err := s.drivers.FindByUserID(ctx, driverUserID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, ErrDriverProfileNotFound
		}
		return nil, fmt.Errorf("service: failed to load driver profile: %w", err)
	}
	if !profile.IsApproved() {
		return nil, ErrDriverNotApproved
	}
	if !profile.HasActiveSubscription() {
		return nil, ErrSubscriptionInactive
	}
	if !profile.IsOnline {
		return nil, ErrNotOnline
	}
	return profile, nil
}

// assertParticipant checks the caller is either the trip's passenger or
// its matched driver.
func (s *TripService) assertParticipant(trip *models.Trip, callerUserID string) error {
	if trip.PassengerID == callerUserID {
		return nil
	}
	if trip.Driver != nil && trip.Driver.UserID == callerUserID {
		return nil
	}
	return ErrNotTripOwner
}

// generatePickupPIN produces a short numeric pickup code plus its bcrypt
// hash for storage. Reuses the same generation/hashing primitives as OTP
// codes — the security properties needed (unpredictable, never stored in
// plaintext) are identical.
func generatePickupPIN() (plain, hash string, err error) {
	plain, err = utils.GenerateOTP(pickupPINLength)
	if err != nil {
		return "", "", err
	}
	hash, err = utils.HashOTP(plain)
	if err != nil {
		return "", "", err
	}
	return plain, hash, nil
}

func (s *TripService) toTripResponse(ctx context.Context, trip *models.Trip) dto.TripResponse {
	resp := dto.TripResponse{
		ID:                 trip.ID,
		PassengerID:        trip.PassengerID,
		VehicleType:        string(trip.VehicleType),
		Status:             string(trip.Status),
		PickupLat:          trip.PickupLat,
		PickupLng:          trip.PickupLng,
		PickupAddress:      trip.PickupAddress,
		DestinationLat:     trip.DestinationLat,
		DestinationLng:     trip.DestinationLng,
		DestinationAddress: trip.DestinationAddress,
		OfferedPriceKobo:   trip.OfferedPriceKobo,
		CreatedAt:          trip.CreatedAt.UTC().Format(time.RFC3339),
	}

	if trip.DriverID != nil {
		resp.DriverID = *trip.DriverID
	}
	if trip.AgreedPriceKobo != nil {
		resp.AgreedPriceKobo = *trip.AgreedPriceKobo
	}
	if trip.PickupConfirmedAt != nil {
		resp.PickupConfirmedAt = trip.PickupConfirmedAt.UTC().Format(time.RFC3339)
	}
	if trip.CompletedAt != nil {
		resp.CompletedAt = trip.CompletedAt.UTC().Format(time.RFC3339)
	}
	if trip.CancelledAt != nil {
		resp.CancelledAt = trip.CancelledAt.UTC().Format(time.RFC3339)
	}
	if trip.CancelledBy != nil {
		resp.CancelledBy = string(*trip.CancelledBy)
	}
	resp.CancelReason = trip.CancelReason

	if trip.Passenger != nil {
		resp.PassengerInfo = &dto.TripPartyInfo{
			Name:     trip.Passenger.Name,
			PhotoURL: s.signPhotoURL(ctx, trip.Passenger.PhotoURL),
			Phone:    trip.Passenger.Phone,
			Email:    trip.Passenger.Email,
			Rating:   trip.Passenger.RatingAverage,
		}
	}
	if trip.Driver != nil {
		info := &dto.TripPartyInfo{
			VehicleType: string(trip.Driver.VehicleType),
			PlateNumber: trip.Driver.PlateNumber,
			Rating:      trip.Driver.RatingAverage,
		}
		if trip.Driver.User != nil {
			info.Name = trip.Driver.User.Name
			info.PhotoURL = s.signPhotoURL(ctx, trip.Driver.User.PhotoURL)
			info.Phone = trip.Driver.User.Phone
			info.Email = trip.Driver.User.Email
		}
		resp.DriverInfo = info
	}

	// Rated-flags only mean anything once the trip is completed — a
	// requested/matched/picked-up trip can't have a rating yet, so skip
	// the two extra lookups for every other status.
	if trip.Status == models.TripCompleted {
		if _, err := s.ratings.FindByTripAndRole(ctx, trip.ID, models.RaterPassenger); err == nil {
			resp.PassengerRated = true
		}
		if _, err := s.ratings.FindByTripAndRole(ctx, trip.ID, models.RaterDriver); err == nil {
			resp.DriverRated = true
		}
	}

	return resp
}

// SubmitRating records the caller's 1-5 rating of the other party on a
// completed trip, then folds it into that party's running average
// (DriverProfile.RatingAverage for a passenger rating their driver,
// User.RatingAverage for a driver rating their passenger) — an
// incremental mean update rather than re-scanning every rating, since the
// count is already tracked alongside the average.
func (s *TripService) SubmitRating(ctx context.Context, callerUserID, tripID string, value int, comment string) (*dto.TripResponse, error) {
	trip, err := s.trips.FindByID(ctx, tripID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, ErrTripNotFound
		}
		return nil, fmt.Errorf("service: failed to load trip: %w", err)
	}
	if trip.Status != models.TripCompleted {
		return nil, ErrTripNotCompleted
	}

	var role models.RaterRole
	switch {
	case trip.PassengerID == callerUserID:
		role = models.RaterPassenger
	case trip.Driver != nil && trip.Driver.UserID == callerUserID:
		role = models.RaterDriver
	default:
		return nil, ErrNotTripOwner
	}

	if _, err := s.ratings.FindByTripAndRole(ctx, tripID, role); err == nil {
		return nil, ErrAlreadyRated
	} else if !errors.Is(err, repository.ErrNotFound) {
		return nil, fmt.Errorf("service: failed to check existing rating: %w", err)
	}

	rating := &models.TripRating{TripID: tripID, RaterUserID: callerUserID, RaterRole: role, Value: value, Comment: comment}
	if err := s.ratings.Create(ctx, rating); err != nil {
		return nil, fmt.Errorf("service: failed to save rating: %w", err)
	}

	if role == models.RaterPassenger {
		// Passenger rates the driver: fold into DriverProfile's average.
		profile := trip.Driver
		if profile == nil {
			profile, err = s.drivers.FindByID(ctx, *trip.DriverID)
			if err != nil {
				return nil, fmt.Errorf("service: failed to load driver for rating: %w", err)
			}
		}
		profile.RatingAverage = incrementalAverage(profile.RatingAverage, profile.RatingCount, value)
		profile.RatingCount++
		if err := s.drivers.Update(ctx, profile); err != nil {
			return nil, fmt.Errorf("service: failed to update driver rating: %w", err)
		}
	} else {
		// Driver rates the passenger: fold into User's average.
		passenger := trip.Passenger
		if passenger == nil {
			passenger, err = s.users.FindByID(ctx, trip.PassengerID)
			if err != nil {
				return nil, fmt.Errorf("service: failed to load passenger for rating: %w", err)
			}
		}
		passenger.RatingAverage = incrementalAverage(passenger.RatingAverage, passenger.RatingCount, value)
		passenger.RatingCount++
		if err := s.users.Update(ctx, passenger); err != nil {
			return nil, fmt.Errorf("service: failed to update passenger rating: %w", err)
		}
	}

	trip, err = s.trips.FindByID(ctx, tripID)
	if err != nil {
		return nil, fmt.Errorf("service: failed to reload trip: %w", err)
	}
	resp := s.toTripResponse(ctx, trip)
	return &resp, nil
}

// incrementalAverage folds one new value into a running mean without
// needing to re-read every prior rating.
func incrementalAverage(currentAverage float64, currentCount int64, newValue int) float64 {
	total := currentAverage*float64(currentCount) + float64(newValue)
	avg := total / float64(currentCount+1)
	return math.Round(avg*100) / 100
}

func (s *TripService) toOfferResponse(ctx context.Context, offer *models.TripOffer, profile *models.DriverProfile) dto.OfferResponse {
	resp := dto.OfferResponse{
		ID:        offer.ID,
		TripID:    offer.TripID,
		PriceKobo: offer.PriceKobo,
		Status:    string(offer.Status),
		CreatedAt: offer.CreatedAt.UTC().Format(time.RFC3339),
	}
	if profile != nil {
		info := &dto.TripPartyInfo{
			VehicleType: string(profile.VehicleType),
			Rating:      profile.RatingAverage,
		}
		if profile.User != nil {
			info.Name = profile.User.Name
			info.PhotoURL = s.signPhotoURL(ctx, profile.User.PhotoURL)
		}
		resp.Driver = info
	}
	return resp
}

// signPhotoURL signs a stored photo URL for the current response — thin
// wrapper around the shared signURL helper (see driver_service.go) so
// call sites in this file read as "sign this trip party's photo" rather
// than repeating the fileStore plumbing.
func (s *TripService) signPhotoURL(ctx context.Context, url string) string {
	return signURL(ctx, s.fileStore, url)
}
