package repository

import (
	"context"
	"errors"

	"gorm.io/gorm"

	"ridematch-backend/internal/models"
)

// DriverFilter narrows an admin driver listing. Zero-value fields are
// ignored (no filter applied on that axis).
type DriverFilter struct {
	VehicleType        string // "car" | "okada" | "keke" | "bus"
	VerificationStatus string // "pending" | "approved" | "rejected"
	Online             *bool
	// Query matches the plate number, or the linked user's name, phone,
	// or email (case-insensitive substring).
	Query string
}

// DriverRepository defines persistence operations for DriverProfile
// records (the driver-specific extension of a User).
type DriverRepository interface {
	Create(ctx context.Context, profile *models.DriverProfile) error
	FindByUserID(ctx context.Context, userID string) (*models.DriverProfile, error)
	FindByPlateNumber(ctx context.Context, plate string) (*models.DriverProfile, error)
	FindByID(ctx context.Context, id string) (*models.DriverProfile, error)
	Update(ctx context.Context, profile *models.DriverProfile) error

	// FindAll returns a filtered, paginated page of driver profiles
	// (newest first) plus the total matching count, for the admin
	// riders list. Always preloads the linked User.
	FindAll(ctx context.Context, filter DriverFilter, limit, offset int) ([]models.DriverProfile, int64, error)

	// CountByVehicleType returns, for every vehicle type that has at
	// least one driver, the number of driver profiles of that type — for
	// the admin dashboard's stat breakdown.
	CountByVehicleType(ctx context.Context) (map[string]int64, error)

	// CountByVerificationStatus mirrors CountByVehicleType, grouped by
	// verification status instead.
	CountByVerificationStatus(ctx context.Context) (map[string]int64, error)

	// CountOnline returns how many drivers are currently marked online.
	CountOnline(ctx context.Context) (int64, error)
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

// filtered returns a fresh query with the given DriverFilter applied —
// called separately for the Count and the Find so neither call's clauses
// leak into the other.
func (r *driverRepository) filtered(ctx context.Context, filter DriverFilter) *gorm.DB {
	q := r.db.WithContext(ctx).Model(&models.DriverProfile{}).
		Joins("JOIN users ON users.id = driver_profiles.user_id AND users.deleted_at IS NULL")

	if filter.VehicleType != "" {
		q = q.Where("driver_profiles.vehicle_type = ?", filter.VehicleType)
	}
	if filter.VerificationStatus != "" {
		q = q.Where("driver_profiles.verification_status = ?", filter.VerificationStatus)
	}
	if filter.Online != nil {
		q = q.Where("driver_profiles.is_online = ?", *filter.Online)
	}
	if filter.Query != "" {
		like := "%" + filter.Query + "%"
		q = q.Where("driver_profiles.plate_number LIKE ? OR users.name LIKE ? OR users.phone LIKE ? OR users.email LIKE ?", like, like, like, like)
	}
	return q
}

func (r *driverRepository) FindAll(ctx context.Context, filter DriverFilter, limit, offset int) ([]models.DriverProfile, int64, error) {
	var total int64
	if err := r.filtered(ctx, filter).Count(&total).Error; err != nil {
		return nil, 0, err
	}

	var profiles []models.DriverProfile
	err := r.filtered(ctx, filter).
		Preload("User").
		Order("driver_profiles.created_at DESC").
		Limit(limit).
		Offset(offset).
		Find(&profiles).Error
	if err != nil {
		return nil, 0, err
	}
	return profiles, total, nil
}

func (r *driverRepository) CountByVehicleType(ctx context.Context) (map[string]int64, error) {
	var rows []struct {
		VehicleType string
		Count       int64
	}
	err := r.db.WithContext(ctx).Model(&models.DriverProfile{}).
		Select("vehicle_type, COUNT(*) AS count").
		Group("vehicle_type").
		Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	result := make(map[string]int64, len(rows))
	for _, row := range rows {
		result[row.VehicleType] = row.Count
	}
	return result, nil
}

func (r *driverRepository) CountByVerificationStatus(ctx context.Context) (map[string]int64, error) {
	var rows []struct {
		VerificationStatus string
		Count              int64
	}
	err := r.db.WithContext(ctx).Model(&models.DriverProfile{}).
		Select("verification_status, COUNT(*) AS count").
		Group("verification_status").
		Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	result := make(map[string]int64, len(rows))
	for _, row := range rows {
		result[row.VerificationStatus] = row.Count
	}
	return result, nil
}

func (r *driverRepository) CountOnline(ctx context.Context) (int64, error) {
	var count int64
	err := r.db.WithContext(ctx).Model(&models.DriverProfile{}).
		Where("is_online = ?", true).
		Count(&count).Error
	return count, err
}
