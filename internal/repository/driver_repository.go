package repository

import (
	"context"
	"errors"

	"gorm.io/gorm"

	"ridematch-backend/internal/models"
)

// DriverRepository defines persistence operations for DriverProfile
// records (the driver-specific extension of a User).
type DriverRepository interface {
	Create(ctx context.Context, profile *models.DriverProfile) error
	FindByUserID(ctx context.Context, userID string) (*models.DriverProfile, error)
	FindByPlateNumber(ctx context.Context, plate string) (*models.DriverProfile, error)
	FindByID(ctx context.Context, id string) (*models.DriverProfile, error)
	Update(ctx context.Context, profile *models.DriverProfile) error
}

type driverRepository struct {
	db *gorm.DB
}

// NewDriverRepository constructs a GORM-backed DriverRepository.
func NewDriverRepository(db *gorm.DB) DriverRepository {
	return &driverRepository{db: db}
}

func (r *driverRepository) Create(ctx context.Context, profile *models.DriverProfile) error {
	return r.db.WithContext(ctx).Create(profile).Error
}

func (r *driverRepository) FindByUserID(ctx context.Context, userID string) (*models.DriverProfile, error) {
	var profile models.DriverProfile
	err := r.db.WithContext(ctx).Preload("User").First(&profile, "user_id = ?", userID).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &profile, nil
}

func (r *driverRepository) FindByPlateNumber(ctx context.Context, plate string) (*models.DriverProfile, error) {
	var profile models.DriverProfile
	err := r.db.WithContext(ctx).First(&profile, "plate_number = ?", plate).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &profile, nil
}

func (r *driverRepository) FindByID(ctx context.Context, id string) (*models.DriverProfile, error) {
	var profile models.DriverProfile
	err := r.db.WithContext(ctx).Preload("User").First(&profile, "id = ?", id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &profile, nil
}

func (r *driverRepository) Update(ctx context.Context, profile *models.DriverProfile) error {
	return r.db.WithContext(ctx).Save(profile).Error
}
