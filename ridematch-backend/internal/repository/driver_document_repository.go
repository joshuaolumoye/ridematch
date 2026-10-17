package repository

import (
	"context"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"ridematch-backend/internal/models"
)

// DriverDocumentRepository defines persistence operations for the files
// a driver submits against each admin-defined DocumentType.
type DriverDocumentRepository interface {
	// UpsertBatch creates or replaces a driver's submitted documents —
	// resubmitting the same document type overwrites the previous file
	// and resets it to pending review, rather than creating a duplicate
	// row (see the uniqueIndex on DriverDocument).
	UpsertBatch(ctx context.Context, docs []models.DriverDocument) error

	// FindByDriverID returns every document a driver has submitted, with
	// its DocumentType preloaded — for the admin driver-detail page.
	FindByDriverID(ctx context.Context, driverID string) ([]models.DriverDocument, error)
}

type driverDocumentRepository struct {
	db *gorm.DB
}

// NewDriverDocumentRepository constructs a GORM-backed DriverDocumentRepository.
func NewDriverDocumentRepository(db *gorm.DB) DriverDocumentRepository {
	return &driverDocumentRepository{db: db}
}

func (r *driverDocumentRepository) UpsertBatch(ctx context.Context, docs []models.DriverDocument) error {
	if len(docs) == 0 {
		return nil
	}
	return r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "driver_id"}, {Name: "document_type_id"}},
		DoUpdates: clause.AssignmentColumns([]string{"file_url", "status", "updated_at"}),
	}).Create(&docs).Error
}

func (r *driverDocumentRepository) FindByDriverID(ctx context.Context, driverID string) ([]models.DriverDocument, error) {
	var rows []models.DriverDocument
	err := r.db.WithContext(ctx).
		Preload("DocumentType").
		Where("driver_id = ?", driverID).
		Find(&rows).Error
	if err != nil {
		return nil, err
	}
	return rows, nil
}
