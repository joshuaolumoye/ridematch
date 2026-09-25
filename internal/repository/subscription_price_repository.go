package repository

import (
	"context"
	"errors"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"ridematch-backend/internal/models"
)

// SubscriptionPriceRepository defines persistence operations for the
// per-vehicle-type daily subscription price admins configure.
type SubscriptionPriceRepository interface {
	// FindAll returns all four vehicle types' prices. If a type has no
	// row yet (shouldn't normally happen — cmd/migrate seeds them all —
	// but a fresh, un-migrated environment could hit this), it's simply
	// omitted rather than erroring.
	FindAll(ctx context.Context) ([]models.SubscriptionPrice, error)

	// FindByVehicleType returns one vehicle type's price.
	FindByVehicleType(ctx context.Context, vehicleType models.VehicleType) (*models.SubscriptionPrice, error)

	// Upsert creates or updates the price for a vehicle type.
	Upsert(ctx context.Context, price *models.SubscriptionPrice) error
}

type subscriptionPriceRepository struct {
	db *gorm.DB
}

// NewSubscriptionPriceRepository constructs a GORM-backed SubscriptionPriceRepository.
func NewSubscriptionPriceRepository(db *gorm.DB) SubscriptionPriceRepository {
	return &subscriptionPriceRepository{db: db}
}

func (r *subscriptionPriceRepository) FindAll(ctx context.Context) ([]models.SubscriptionPrice, error) {
	var prices []models.SubscriptionPrice
	err := r.db.WithContext(ctx).Order("vehicle_type ASC").Find(&prices).Error
	if err != nil {
		return nil, err
	}
	return prices, nil
}

func (r *subscriptionPriceRepository) FindByVehicleType(ctx context.Context, vehicleType models.VehicleType) (*models.SubscriptionPrice, error) {
	var price models.SubscriptionPrice
	err := r.db.WithContext(ctx).First(&price, "vehicle_type = ?", vehicleType).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &price, nil
}

func (r *subscriptionPriceRepository) Upsert(ctx context.Context, price *models.SubscriptionPrice) error {
	return r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "vehicle_type"}},
		DoUpdates: clause.AssignmentColumns([]string{"price_kobo_per_day", "updated_at"}),
	}).Create(price).Error
}
