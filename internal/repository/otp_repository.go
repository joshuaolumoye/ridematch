package repository

import (
	"context"
	"errors"
	"time"

	"gorm.io/gorm"

	"ridematch-backend/internal/models"
)

// OTPRepository defines persistence operations for one-time-password
// challenges.
type OTPRepository interface {
	Create(ctx context.Context, otp *models.OTPRequest) error
	// FindLatestActive returns the most recent, still-usable (not expired,
	// not consumed) OTP issued for a phone number and purpose.
	FindLatestActive(ctx context.Context, phone string, purpose models.OTPPurpose) (*models.OTPRequest, error)
	// FindLatestAny returns the most recent OTP regardless of validity,
	// used to enforce the resend cooldown.
	FindLatestAny(ctx context.Context, phone string, purpose models.OTPPurpose) (*models.OTPRequest, error)
	Update(ctx context.Context, otp *models.OTPRequest) error
	// DeleteExpired removes stale rows older than the given cutoff; intended
	// to be called periodically (e.g. from a cron job) to keep the table
	// small. Not required for correctness, only housekeeping.
	DeleteExpired(ctx context.Context, olderThan time.Time) error
}

type otpRepository struct {
	db *gorm.DB
}

// NewOTPRepository constructs a GORM-backed OTPRepository.
func NewOTPRepository(db *gorm.DB) OTPRepository {
	return &otpRepository{db: db}
}

func (r *otpRepository) Create(ctx context.Context, otp *models.OTPRequest) error {
	return r.db.WithContext(ctx).Create(otp).Error
}

func (r *otpRepository) FindLatestActive(ctx context.Context, phone string, purpose models.OTPPurpose) (*models.OTPRequest, error) {
	var otp models.OTPRequest
	err := r.db.WithContext(ctx).
		Where("phone = ? AND purpose = ? AND consumed_at IS NULL AND expires_at > ?", phone, purpose, time.Now()).
		Order("created_at DESC").
		First(&otp).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &otp, nil
}

func (r *otpRepository) FindLatestAny(ctx context.Context, phone string, purpose models.OTPPurpose) (*models.OTPRequest, error) {
	var otp models.OTPRequest
	err := r.db.WithContext(ctx).
		Where("phone = ? AND purpose = ?", phone, purpose).
		Order("created_at DESC").
		First(&otp).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &otp, nil
}

func (r *otpRepository) Update(ctx context.Context, otp *models.OTPRequest) error {
	return r.db.WithContext(ctx).Save(otp).Error
}

func (r *otpRepository) DeleteExpired(ctx context.Context, olderThan time.Time) error {
	return r.db.WithContext(ctx).
		Unscoped().
		Where("expires_at < ?", olderThan).
		Delete(&models.OTPRequest{}).Error
}
