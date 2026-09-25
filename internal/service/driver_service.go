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
)

// DriverService implements driver registration, going online/offline,
// live location updates, and the nearby-drivers query passengers use to
// browse the map.
type DriverService struct {
	drivers               repository.DriverRepository
	users                 repository.UserRepository
	locations             repository.LocationRepository
	fileStore             storage.Store
	locationStaleAfter    time.Duration
	nearbyDefaultRadiusKM float64
	nearbyMaxRadiusKM     float64
	nearbyDefaultLimit    int
	coordinatePrecision   int
}

// NewDriverService constructs a DriverService from its dependencies.
func NewDriverService(
	drivers repository.DriverRepository,
	users repository.UserRepository,
	locations repository.LocationRepository,
	fileStore storage.Store,
	locationStaleAfter time.Duration,
	nearbyDefaultRadiusKM, nearbyMaxRadiusKM float64,
	nearbyDefaultLimit, coordinatePrecision int,
) *DriverService {
	return &DriverService{
		drivers:               drivers,
		users:                 users,
		locations:             locations,
		fileStore:             fileStore,
		locationStaleAfter:    locationStaleAfter,
		nearbyDefaultRadiusKM: nearbyDefaultRadiusKM,
		nearbyMaxRadiusKM:     nearbyMaxRadiusKM,
		nearbyDefaultLimit:    nearbyDefaultLimit,
		coordinatePrecision:   coordinatePrecision,
	}
}

// Register creates a DriverProfile for a user and promotes their account
// role to "driver". A user may register at most one vehicle.
func (s *DriverService) Register(ctx context.Context, userID string, req dto.RegisterDriverRequest) (*dto.DriverProfileResponse, error) {
	if _, err := s.drivers.FindByUserID(ctx, userID); err == nil {
		return nil, ErrDriverAlreadyRegistered
	} else if !errors.Is(err, repository.ErrNotFound) {
		return nil, fmt.Errorf("service: failed to check existing driver profile: %w", err)
	}

	if _, err := s.drivers.FindByPlateNumber(ctx, req.PlateNumber); err == nil {
		return nil, ErrPlateNumberTaken
	} else if !errors.Is(err, repository.ErrNotFound) {
		return nil, fmt.Errorf("service: failed to check plate number: %w", err)
	}

	profile := &models.DriverProfile{
		UserID:             userID,
		VehicleType:        models.VehicleType(req.VehicleType),
		PlateNumber:        req.PlateNumber,
		VehiclePhotoURL:    req.VehiclePhotoURL,
		IDDocumentURL:      req.IDDocumentURL,
		VerificationStatus: models.VerificationPending,
	}
	if err := s.drivers.Create(ctx, profile); err != nil {
		return nil, fmt.Errorf("service: failed to create driver profile: %w", err)
	}

	user, err := s.users.FindByID(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("service: failed to load user after driver registration: %w", err)
	}
	user.Role = models.RoleDriver
	if err := s.users.Update(ctx, user); err != nil {
		return nil, fmt.Errorf("service: failed to promote user to driver role: %w", err)
	}

	resp := s.toDriverProfileResponse(ctx, profile)
	return &resp, nil
}

// GetMyProfile returns the caller's own driver profile.
func (s *DriverService) GetMyProfile(ctx context.Context, userID string) (*dto.DriverProfileResponse, error) {
	profile, err := s.drivers.FindByUserID(ctx, userID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, ErrDriverProfileNotFound
		}
		return nil, fmt.Errorf("service: failed to load driver profile: %w", err)
	}
	resp := s.toDriverProfileResponse(ctx, profile)
	return &resp, nil
}

// GoOnline marks a driver online and publishes their live position, after
// confirming they've passed verification and have an active subscription.
func (s *DriverService) GoOnline(ctx context.Context, userID string, lat, lng float64) (*dto.DriverProfileResponse, error) {
	profile, err := s.drivers.FindByUserID(ctx, userID)
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

	if err := s.locations.UpdateLocation(ctx, profile.ID, profile.VehicleType, lat, lng); err != nil {
		return nil, err
	}

	profile.IsOnline = true
	if err := s.drivers.Update(ctx, profile); err != nil {
		return nil, fmt.Errorf("service: failed to mark driver online: %w", err)
	}

	resp := s.toDriverProfileResponse(ctx, profile)
	return &resp, nil
}

// GoOffline marks a driver offline and removes their live position from
// the nearby-matching index.
func (s *DriverService) GoOffline(ctx context.Context, userID string) error {
	profile, err := s.drivers.FindByUserID(ctx, userID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return ErrDriverProfileNotFound
		}
		return fmt.Errorf("service: failed to load driver profile: %w", err)
	}

	if err := s.locations.RemoveLocation(ctx, profile.ID, profile.VehicleType); err != nil {
		return err
	}

	profile.IsOnline = false
	if err := s.drivers.Update(ctx, profile); err != nil {
		return fmt.Errorf("service: failed to mark driver offline: %w", err)
	}
	return nil
}

// PingLocation updates an already-online driver's live position. Called
// periodically by the app (e.g. every 5-10 seconds) while online.
func (s *DriverService) PingLocation(ctx context.Context, userID string, lat, lng float64) error {
	profile, err := s.drivers.FindByUserID(ctx, userID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return ErrDriverProfileNotFound
		}
		return fmt.Errorf("service: failed to load driver profile: %w", err)
	}
	if !profile.IsOnline {
		return ErrNotOnline
	}

	return s.locations.UpdateLocation(ctx, profile.ID, profile.VehicleType, lat, lng)
}

// FindNearbyDrivers returns online drivers of the given vehicle type near
// a point, for a passenger browsing the map. Coordinates in the response
// are rounded to reduce live-tracking exposure; exact position is only
// shared once a match is confirmed (a later milestone).
func (s *DriverService) FindNearbyDrivers(ctx context.Context, vehicleType string, lat, lng float64, radiusKM float64, limit int) ([]dto.NearbyDriverResponse, error) {
	if radiusKM <= 0 {
		radiusKM = s.nearbyDefaultRadiusKM
	}
	if radiusKM > s.nearbyMaxRadiusKM {
		radiusKM = s.nearbyMaxRadiusKM
	}
	if limit <= 0 {
		limit = s.nearbyDefaultLimit
	}

	nearby, err := s.locations.FindNearby(ctx, models.VehicleType(vehicleType), lat, lng, radiusKM, limit, s.locationStaleAfter)
	if err != nil {
		return nil, err
	}
	if len(nearby) == 0 {
		return []dto.NearbyDriverResponse{}, nil
	}

	results := make([]dto.NearbyDriverResponse, 0, len(nearby))
	for _, n := range nearby {
		profile, err := s.drivers.FindByID(ctx, n.DriverID)
		if err != nil {
			// A driver present in Redis but missing/deleted in MySQL is a
			// data-consistency edge case, not a request failure — skip it
			// rather than failing the whole nearby list.
			continue
		}

		name := ""
		photoURL := ""
		if profile.User != nil {
			name = profile.User.Name
			photoURL = signURL(ctx, s.fileStore, profile.User.PhotoURL)
		}

		results = append(results, dto.NearbyDriverResponse{
			DriverID:      profile.ID,
			Name:          name,
			PhotoURL:      photoURL,
			VehicleType:   string(profile.VehicleType),
			RatingAverage: profile.RatingAverage,
			DistanceKM:    roundTo(n.DistanceKM, 2),
			Latitude:      roundTo(n.Latitude, s.coordinatePrecision),
			Longitude:     roundTo(n.Longitude, s.coordinatePrecision),
		})
	}

	return results, nil
}

func roundTo(v float64, places int) float64 {
	mult := math.Pow(10, float64(places))
	return math.Round(v*mult) / mult
}

func (s *DriverService) toDriverProfileResponse(ctx context.Context, p *models.DriverProfile) dto.DriverProfileResponse {
	return buildDriverProfileResponse(p, signURL(ctx, s.fileStore, p.VehiclePhotoURL))
}

// signURL signs a stored URL for the current response if it's non-empty,
// falling back to the unsigned URL on a signing error (a photo/document
// that 403s is a smaller problem than an API call failing outright over
// it). Shared by every service that puts a photo or document URL into a
// response.
func signURL(ctx context.Context, fileStore storage.Store, url string) string {
	if url == "" {
		return url
	}
	if signed, err := fileStore.SignedURL(ctx, url); err == nil {
		return signed
	}
	return url
}

// buildDriverProfileResponse assembles the response from an already-
// resolved (signed) vehicle photo URL — split out from
// DriverService.toDriverProfileResponse so AdminService, which edits the
// same profiles but isn't otherwise a DriverService, can build the exact
// same shape rather than hand-rolling its own copy.
func buildDriverProfileResponse(p *models.DriverProfile, vehiclePhotoURL string) dto.DriverProfileResponse {
	resp := dto.DriverProfileResponse{
		ID:                    p.ID,
		UserID:                p.UserID,
		VehicleType:           string(p.VehicleType),
		PlateNumber:           p.PlateNumber,
		VehiclePhotoURL:       vehiclePhotoURL,
		VerificationStatus:    string(p.VerificationStatus),
		VerificationNote:      p.VerificationNote,
		IsOnline:              p.IsOnline,
		HasActiveSubscription: p.HasActiveSubscription(),
		RatingAverage:         p.RatingAverage,
	}
	if p.SubscriptionActiveUntil != nil {
		resp.SubscriptionActiveUntil = p.SubscriptionActiveUntil.UTC().Format(time.RFC3339)
	}
	return resp
}
