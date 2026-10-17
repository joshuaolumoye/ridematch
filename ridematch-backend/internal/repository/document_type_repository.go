package repository

import (
	"context"
	"errors"

	"gorm.io/gorm"

	"ridematch-backend/internal/models"
)

// DocumentTypeRepository defines persistence operations for the
// admin-defined, per-vehicle-type list of documents a driver must (or
// may optionally) submit at registration.
type DocumentTypeRepository interface {
	// FindAll returns every document type, across every vehicle type,
	// for the admin settings page's full list.
	FindAll(ctx context.Context) ([]models.DocumentType, error)

	// FindByVehicleType returns one vehicle type's document types,
	// enabled and disabled alike, in SortOrder — for the admin settings
	// page's per-vehicle-type editor.
	FindByVehicleType(ctx context.Context, vehicleType models.VehicleType) ([]models.DocumentType, error)

	// FindEnabledByVehicleType returns only the enabled document types
	// for one vehicle type, in SortOrder — what the registration screen
	// actually renders.
	FindEnabledByVehicleType(ctx context.Context, vehicleType models.VehicleType) ([]models.DocumentType, error)

	FindByID(ctx context.Context, id string) (*models.DocumentType, error)

	Create(ctx context.Context, docType *models.DocumentType) error
	Update(ctx context.Context, docType *models.DocumentType) error
	Delete(ctx context.Context, id string) error
}

type documentTypeRepository struct {
	db *gorm.DB
}

// NewDocumentTypeRepository constructs a GORM-backed DocumentTypeRepository.
func NewDocumentTypeRepository(db *gorm.DB) DocumentTypeRepository {
	return &documentTypeRepository{db: db}
}

func (r *documentTypeRepository) FindAll(ctx context.Context) ([]models.DocumentType, error) {
	var rows []models.DocumentType
	err := r.db.WithContext(ctx).Order("vehicle_type ASC, sort_order ASC, created_at ASC").Find(&rows).Error
	if err != nil {
		return nil, err
	}
	return rows, nil
}

func (r *documentTypeRepository) FindByVehicleType(ctx context.Context, vehicleType models.VehicleType) ([]models.DocumentType, error) {
	var rows []models.DocumentType
	err := r.db.WithContext(ctx).
		Where("vehicle_type = ?", vehicleType).
		Order("sort_order ASC, created_at ASC").
		Find(&rows).Error
	if err != nil {
		return nil, err
	}
	return rows, nil
}

func (r *documentTypeRepository) FindEnabledByVehicleType(ctx context.Context, vehicleType models.VehicleType) ([]models.DocumentType, error) {
	var rows []models.DocumentType
	err := r.db.WithContext(ctx).
		Where("vehicle_type = ? AND enabled = ?", vehicleType, true).
		Order("sort_order ASC, created_at ASC").
		Find(&rows).Error
	if err != nil {
		return nil, err
	}
	return rows, nil
}

func (r *documentTypeRepository) FindByID(ctx context.Context, id string) (*models.DocumentType, error) {
	var row models.DocumentType
	err := r.db.WithContext(ctx).First(&row, "id = ?", id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &row, nil
}

func (r *documentTypeRepository) Create(ctx context.Context, docType *models.DocumentType) error {
	return r.db.WithContext(ctx).Create(docType).Error
}

func (r *documentTypeRepository) Update(ctx context.Context, docType *models.DocumentType) error {
	return r.db.WithContext(ctx).Save(docType).Error
}

func (r *documentTypeRepository) Delete(ctx context.Context, id string) error {
	return r.db.WithContext(ctx).Delete(&models.DocumentType{}, "id = ?", id).Error
}
