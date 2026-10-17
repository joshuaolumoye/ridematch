package repository

import (
	"context"
	"errors"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"ridematch-backend/internal/models"
)

// SubscriptionPlanRepository defines persistence operations for the
// admin-configurable (vehicle type, billing cycle) price/enabled grid.
type SubscriptionPlanRepository interface {
	// FindAll returns every plan row across all vehicle types and cycles
	// (up to 4 x 4 = 16, once seeded), for the admin settings page.
	FindAll(ctx context.Context) ([]models.SubscriptionPlan, error)

	// FindByVehicleType returns every cycle's plan for one vehicle type
	// (enabled and disabled alike) — the admin's per-vehicle-type settings
	// card needs to show and let them toggle all four.
	FindByVehicleType(ctx context.Context, vehicleType models.VehicleType) ([]models.SubscriptionPlan, error)

	// FindEnabledByVehicleType returns only the enabled cycles for one
	// vehicle type — what a driver's app is allowed to offer at checkout.
	FindEnabledByVehicleType(ctx context.Context, vehicleType models.VehicleType) ([]models.SubscriptionPlan, error)

	FindByVehicleTypeAndCycle(ctx context.Context, vehicleType models.VehicleType, cycle models.BillingCycle) (*models.SubscriptionPlan, error)

	// Upsert creates or updates one (vehicle type, billing cycle) plan.
	Upsert(ctx context.Context, plan *models.SubscriptionPlan) error
}

type subscriptionPlanRepository struct {
	db *gorm.DB
}

// NewSubscriptionPlanRepository constructs a GORM-backed SubscriptionPlanRepository.
func NewSubscriptionPlanRepository(db *gorm.DB) SubscriptionPlanRepository {
	return &subscriptionPlanRepository{db: db}
}

func (r *subscriptionPlanRepository) FindAll(ctx context.Context) ([]models.SubscriptionPlan, error) {
	var rows []models.SubscriptionPlan
	err := r.db.WithContext(ctx).Order("vehicle_type ASC, billing_cycle ASC").Find(&rows).Error
	if err != nil {
		return nil, err
	}
	return rows, nil
}

func (r *subscriptionPlanRepository) FindByVehicleType(ctx context.Context, vehicleType models.VehicleType) ([]models.SubscriptionPlan, error) {
	var rows []models.SubscriptionPlan
	err := r.db.WithContext(ctx).Where("vehicle_type = ?", vehicleType).Order("billing_cycle ASC").Find(&rows).Error
	if err != nil {
		return nil, err
	}
	return rows, nil
}

func (r *subscriptionPlanRepository) FindEnabledByVehicleType(ctx context.Context, vehicleType models.VehicleType) ([]models.SubscriptionPlan, error) {
	var rows []models.SubscriptionPlan
	err := r.db.WithContext(ctx).
		Where("vehicle_type = ? AND enabled = ?", vehicleType, true).
		Order("billing_cycle ASC").
		Find(&rows).Error
	if err != nil {
		return nil, err
	}
	return rows, nil
}

func (r *subscriptionPlanRepository) FindByVehicleTypeAndCycle(ctx context.Context, vehicleType models.VehicleType, cycle models.BillingCycle) (*models.SubscriptionPlan, error) {
	var row models.SubscriptionPlan
	err := r.db.WithContext(ctx).First(&row, "vehicle_type = ? AND billing_cycle = ?", vehicleType, cycle).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &row, nil
}

func (r *subscriptionPlanRepository) Upsert(ctx context.Context, plan *models.SubscriptionPlan) error {
	return r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "vehicle_type"}, {Name: "billing_cycle"}},
		DoUpdates: clause.AssignmentColumns([]string{"enabled", "price_kobo", "updated_at"}),
	}).Create(plan).Error
}
