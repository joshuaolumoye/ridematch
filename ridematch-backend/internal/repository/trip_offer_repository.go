package repository

import (
	"context"
	"errors"

	"gorm.io/gorm"

	"ridematch-backend/internal/models"
)

// TripOfferRepository defines persistence operations for driver offers
// against a trip request.
type TripOfferRepository interface {
	Create(ctx context.Context, offer *models.TripOffer) error
	FindByID(ctx context.Context, id string) (*models.TripOffer, error)
	FindPendingByTripAndDriver(ctx context.Context, tripID, driverID string) (*models.TripOffer, error)
	Update(ctx context.Context, offer *models.TripOffer) error

	// SupersedeOtherPending marks every other pending offer on a trip as
	// superseded in a single statement — used the moment one offer is
	// accepted, so every other driver's UI reflects "trip closed"
	// immediately rather than after N individual updates.
	SupersedeOtherPending(ctx context.Context, tripID, acceptedOfferID string) error

	// WithdrawAllPending marks every pending offer on a trip as withdrawn
	// (trip cancelled or expired while offers were outstanding).
	WithdrawAllPending(ctx context.Context, tripID string) error

	// PendingDriverIDs returns the driver IDs with a currently pending
	// offer on a trip — used to know who to notify before their offers
	// are superseded/withdrawn.
	PendingDriverIDs(ctx context.Context, tripID string) ([]string, error)

	// FindPendingByTrip returns every currently pending offer on a trip,
	// with driver details preloaded, for the passenger to review.
	FindPendingByTrip(ctx context.Context, tripID string) ([]models.TripOffer, error)

	// CompareAndSwapStatus atomically transitions an offer's status only
	// if it still matches expectedStatus — see TripRepository's method of
	// the same name for why this matters under concurrency.
	CompareAndSwapStatus(ctx context.Context, offerID string, expectedStatus, newStatus models.TripOfferStatus) (bool, error)
}

type tripOfferRepository struct {
	db *gorm.DB
}

// NewTripOfferRepository constructs a GORM-backed TripOfferRepository.
func NewTripOfferRepository(db *gorm.DB) TripOfferRepository {
	return &tripOfferRepository{db: db}
}

func (r *tripOfferRepository) Create(ctx context.Context, offer *models.TripOffer) error {
	return r.db.WithContext(ctx).Create(offer).Error
}

func (r *tripOfferRepository) FindByID(ctx context.Context, id string) (*models.TripOffer, error) {
	var offer models.TripOffer
	err := r.db.WithContext(ctx).Preload("Driver.User").First(&offer, "id = ?", id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &offer, nil
}

func (r *tripOfferRepository) FindPendingByTripAndDriver(ctx context.Context, tripID, driverID string) (*models.TripOffer, error) {
	var offer models.TripOffer
	err := r.db.WithContext(ctx).
		Where("trip_id = ? AND driver_id = ? AND status = ?", tripID, driverID, models.OfferPending).
		First(&offer).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &offer, nil
}

func (r *tripOfferRepository) Update(ctx context.Context, offer *models.TripOffer) error {
	return r.db.WithContext(ctx).Save(offer).Error
}

func (r *tripOfferRepository) SupersedeOtherPending(ctx context.Context, tripID, acceptedOfferID string) error {
	return r.db.WithContext(ctx).
		Model(&models.TripOffer{}).
		Where("trip_id = ? AND id <> ? AND status = ?", tripID, acceptedOfferID, models.OfferPending).
		Update("status", models.OfferSuperseded).Error
}

func (r *tripOfferRepository) FindPendingByTrip(ctx context.Context, tripID string) ([]models.TripOffer, error) {
	var offers []models.TripOffer
	err := r.db.WithContext(ctx).
		Preload("Driver.User").
		Where("trip_id = ? AND status = ?", tripID, models.OfferPending).
		Order("created_at ASC").
		Find(&offers).Error
	return offers, err
}

func (r *tripOfferRepository) CompareAndSwapStatus(ctx context.Context, offerID string, expectedStatus, newStatus models.TripOfferStatus) (bool, error) {
	result := r.db.WithContext(ctx).
		Model(&models.TripOffer{}).
		Where("id = ? AND status = ?", offerID, expectedStatus).
		Update("status", newStatus)
	if result.Error != nil {
		return false, result.Error
	}
	return result.RowsAffected > 0, nil
}

func (r *tripOfferRepository) PendingDriverIDs(ctx context.Context, tripID string) ([]string, error) {
	var ids []string
	err := r.db.WithContext(ctx).
		Model(&models.TripOffer{}).
		Where("trip_id = ? AND status = ?", tripID, models.OfferPending).
		Pluck("driver_id", &ids).Error
	return ids, err
}

func (r *tripOfferRepository) WithdrawAllPending(ctx context.Context, tripID string) error {
	return r.db.WithContext(ctx).
		Model(&models.TripOffer{}).
		Where("trip_id = ? AND status = ?", tripID, models.OfferPending).
		Update("status", models.OfferWithdrawn).Error
}
