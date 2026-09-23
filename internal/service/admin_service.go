package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"ridematch-backend/internal/dto"
	"ridematch-backend/internal/models"
	"ridematch-backend/internal/repository"
)

// AdminService implements staff-only operations: driver document
// verification and subscription activation. ActivateSubscription is
// deliberately generic (driver ID + duration) so the future Flutterwave
// webhook handler can call the exact same method an admin's manual
// override uses today — one code path, two callers.
type AdminService struct {
	drivers repository.DriverRepository
}

// NewAdminService constructs an AdminService.
func NewAdminService(drivers repository.DriverRepository) *AdminService {
	return &AdminService{drivers: drivers}
}

// VerifyDriver approves or rejects a driver's submitted documents.
func (s *AdminService) VerifyDriver(ctx context.Context, driverID string, req dto.AdminVerifyDriverRequest) (*dto.DriverProfileResponse, error) {
	profile, err := s.drivers.FindByID(ctx, driverID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, ErrDriverProfileNotFound
		}
		return nil, fmt.Errorf("service: failed to load driver profile: %w", err)
	}

	if req.Approve {
		profile.VerificationStatus = models.VerificationApproved
	} else {
		profile.VerificationStatus = models.VerificationRejected
	}
	profile.VerificationNote = req.Note

	if err := s.drivers.Update(ctx, profile); err != nil {
		return nil, fmt.Errorf("service: failed to update verification status: %w", err)
	}

	resp := toDriverProfileResponse(profile)
	return &resp, nil
}

// ActivateSubscription extends (or starts) a driver's platform-access
// subscription by `days` from now, or from their current expiry if it
// hasn't lapsed yet — so renewing before expiry stacks rather than
// wasting the remaining paid time.
func (s *AdminService) ActivateSubscription(ctx context.Context, driverID string, days int) (*dto.DriverProfileResponse, error) {
	profile, err := s.drivers.FindByID(ctx, driverID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, ErrDriverProfileNotFound
		}
		return nil, fmt.Errorf("service: failed to load driver profile: %w", err)
	}

	base := time.Now()
	if profile.HasActiveSubscription() {
		base = *profile.SubscriptionActiveUntil
	}
	newExpiry := base.Add(time.Duration(days) * 24 * time.Hour)
	profile.SubscriptionActiveUntil = &newExpiry

	if err := s.drivers.Update(ctx, profile); err != nil {
		return nil, fmt.Errorf("service: failed to activate subscription: %w", err)
	}

	resp := toDriverProfileResponse(profile)
	return &resp, nil
}
