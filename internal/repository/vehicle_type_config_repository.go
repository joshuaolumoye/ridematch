package repository

import (
	"context"
	"errors"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"ridematch-backend/internal/models"
)

// VehicleTypeConfigRepository defines persistence operations for the
// admin-configurable enable/disable switch on each vehicle type.
type VehicleTypeConfigRepository interface {
	// FindAll returns every vehicle type that has a config row. A fresh,
	// un-migrated environment (before cmd/migrate's seed runs) could
	// return fewer than four — callers should treat a missing type as
	// "enabled" (see AllBillingCycles-style defaulting in the service
	// layer), not as an error.
	FindAll(ctx context.Context) ([]models.VehicleTypeConfig, error)

	FindByVehicleType(ctx context.Context, vehicleType models.VehicleType) (*models.VehicleTypeConfig, error)

	// Upsert creates or updates one vehicle type's enabled flag.
	Upsert(ctx context.Context, cfg *models.VehicleTypeConfig) error
}

type vehicleTypeConfigRepository struct {
	db *gorm.DB
}

// NewVehicleTypeConfigRepository constructs a GORM-backed VehicleTypeConfigRepository.
func NewVehicleTypeConfigRepository(db *gorm.DB) VehicleTypeConfigRepository {
	return &vehicleTypeConfigRepository{db: db}
}

func (r *vehicleTypeConfigRepository) FindAll(ctx context.Context) ([]models.VehicleTypeConfig, error) {
	var rows []models.VehicleTypeConfig
	err := r.db.WithContext(ctx).Order("vehicle_type ASC").Find(&rows).Error
	if err != nil {
		return nil, err
	}
	return rows, nil
}

func (r *vehicleTypeConfigRepository) FindByVehicleType(ctx context.Context, vehicleType models.VehicleType) (*models.VehicleTypeConfig, error) {
	var row models.VehicleTypeConfig
	err := r.db.WithContext(ctx).First(&row, "vehicle_type = ?", vehicleType).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &row, nil
}

func (r *vehicleTypeConfigRepository) Upsert(ctx context.Context, cfg *models.VehicleTypeConfig) error {
	return r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "vehicle_type"}},
		DoUpdates: clause.AssignmentColumns([]string{"enabled", "updated_at"}),
	}).Create(cfg).Error
}
