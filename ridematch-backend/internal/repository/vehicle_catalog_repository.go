package repository

import (
	"context"
	"errors"
	"strings"

	"github.com/go-sql-driver/mysql"
	"gorm.io/gorm"

	"ridematch-backend/internal/models"
)

// ErrDuplicate is returned by CreateMake/CreateModel when the insert
// collides with the make/model's unique index — either a genuine
// duplicate the FindByName pre-check missed (a race between two
// requests), or a leftover soft-deleted row occupying the same
// (vehicle_type, name) slot (see the comment on DeleteMake/DeleteModel
// below for why that stopped being possible once those switched to a
// hard delete).
var ErrDuplicate = errors.New("repository: duplicate entry")

// isDuplicateKeyErr reports whether err is a MySQL "Duplicate entry"
// error (1062) — the unique-index violation CreateMake/CreateModel can
// hit.
func isDuplicateKeyErr(err error) bool {
	var mysqlErr *mysql.MySQLError
	return errors.As(err, &mysqlErr) && mysqlErr.Number == 1062
}

// VehicleCatalogRepository persists the admin-curated make/model catalog
// drivers pick their vehicle from at registration, and that the
// passenger booking screen's category icons read from.
type VehicleCatalogRepository interface {
	// FindMakesByVehicleType returns every make (enabled and disabled)
	// for one vehicle type, with its models preloaded, in name order.
	FindMakesByVehicleType(ctx context.Context, vehicleType models.VehicleType) ([]models.VehicleMake, error)

	FindMakeByID(ctx context.Context, id string) (*models.VehicleMake, error)
	// FindMakeByName does a case-insensitive lookup within one vehicle
	// type — used by both the one-at-a-time and bulk-import paths so
	// re-importing the same make never creates a duplicate.
	FindMakeByName(ctx context.Context, vehicleType models.VehicleType, name string) (*models.VehicleMake, error)
	CreateMake(ctx context.Context, make_ *models.VehicleMake) error
	UpdateMake(ctx context.Context, make_ *models.VehicleMake) error
	DeleteMake(ctx context.Context, id string) error

	FindModelByID(ctx context.Context, id string) (*models.VehicleModel, error)
	FindModelByName(ctx context.Context, makeID string, name string) (*models.VehicleModel, error)
	CreateModel(ctx context.Context, model *models.VehicleModel) error
	UpdateModel(ctx context.Context, model *models.VehicleModel) error
	DeleteModel(ctx context.Context, id string) error
}

type vehicleCatalogRepository struct {
	db *gorm.DB
}

// NewVehicleCatalogRepository constructs a GORM-backed
// VehicleCatalogRepository.
func NewVehicleCatalogRepository(db *gorm.DB) VehicleCatalogRepository {
	return &vehicleCatalogRepository{db: db}
}

func (r *vehicleCatalogRepository) FindMakesByVehicleType(ctx context.Context, vehicleType models.VehicleType) ([]models.VehicleMake, error) {
	var rows []models.VehicleMake
	err := r.db.WithContext(ctx).
		Preload("Models", func(db *gorm.DB) *gorm.DB { return db.Order("name ASC") }).
		Where("vehicle_type = ?", vehicleType).
		Order("name ASC").
		Find(&rows).Error
	if err != nil {
		return nil, err
	}
	return rows, nil
}

func (r *vehicleCatalogRepository) FindMakeByID(ctx context.Context, id string) (*models.VehicleMake, error) {
	var row models.VehicleMake
	err := r.db.WithContext(ctx).First(&row, "id = ?", id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &row, nil
}

func (r *vehicleCatalogRepository) FindMakeByName(ctx context.Context, vehicleType models.VehicleType, name string) (*models.VehicleMake, error) {
	var row models.VehicleMake
	err := r.db.WithContext(ctx).
		Where("vehicle_type = ? AND LOWER(name) = LOWER(?)", vehicleType, strings.TrimSpace(name)).
		First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &row, nil
}

func (r *vehicleCatalogRepository) CreateMake(ctx context.Context, make_ *models.VehicleMake) error {
	if err := r.db.WithContext(ctx).Create(make_).Error; err != nil {
		if isDuplicateKeyErr(err) {
			return ErrDuplicate
		}
		return err
	}
	return nil
}

func (r *vehicleCatalogRepository) UpdateMake(ctx context.Context, make_ *models.VehicleMake) error {
	return r.db.WithContext(ctx).Save(make_).Error
}

// DeleteMake hard-deletes a make and its models (Unscoped — bypassing
// the normal soft delete). Vehicle_makes/vehicle_models are looked up by
// a (vehicle_type, name) / (make_id, name) unique index that doesn't
// exclude soft-deleted rows, so a soft delete here would leave a ghost
// row behind that silently blocks ever recreating that same make/model
// — exactly the bug this fixes. This is safe because nothing else
// depends on these rows surviving deletion: a driver's chosen
// make/model/category is snapshotted onto their profile at registration
// (see models.DriverProfile), not just referenced by ID.
func (r *vehicleCatalogRepository) DeleteMake(ctx context.Context, id string) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Unscoped().Where("make_id = ?", id).Delete(&models.VehicleModel{}).Error; err != nil {
			return err
		}
		return tx.Unscoped().Delete(&models.VehicleMake{}, "id = ?", id).Error
	})
}

func (r *vehicleCatalogRepository) FindModelByID(ctx context.Context, id string) (*models.VehicleModel, error) {
	var row models.VehicleModel
	err := r.db.WithContext(ctx).First(&row, "id = ?", id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &row, nil
}

func (r *vehicleCatalogRepository) FindModelByName(ctx context.Context, makeID string, name string) (*models.VehicleModel, error) {
	var row models.VehicleModel
	err := r.db.WithContext(ctx).
		Where("make_id = ? AND LOWER(name) = LOWER(?)", makeID, strings.TrimSpace(name)).
		First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &row, nil
}

func (r *vehicleCatalogRepository) CreateModel(ctx context.Context, model *models.VehicleModel) error {
	if err := r.db.WithContext(ctx).Create(model).Error; err != nil {
		if isDuplicateKeyErr(err) {
			return ErrDuplicate
		}
		return err
	}
	return nil
}

func (r *vehicleCatalogRepository) UpdateModel(ctx context.Context, model *models.VehicleModel) error {
	return r.db.WithContext(ctx).Save(model).Error
}

// DeleteModel hard-deletes (Unscoped) for the same reason as DeleteMake
// above — its unique index doesn't exclude soft-deleted rows.
func (r *vehicleCatalogRepository) DeleteModel(ctx context.Context, id string) error {
	return r.db.WithContext(ctx).Unscoped().Delete(&models.VehicleModel{}, "id = ?", id).Error
}
