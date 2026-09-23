package repository

import (
	"context"
	"errors"

	"gorm.io/gorm"

	"ridematch-backend/internal/models"
)

// TripRatingRepository defines persistence operations for TripRating
// records — one party's rating of the other on a completed trip.
type TripRatingRepository interface {
	Create(ctx context.Context, rating *models.TripRating) error

	// FindByTripAndRole looks up the rating a specific side (passenger or
	// driver) has already submitted for a trip, if any. Used both to
	// reject a duplicate submission and to report "have I already rated
	// this?" back to the client.
	FindByTripAndRole(ctx context.Context, tripID string, role models.RaterRole) (*models.TripRating, error)
}

type tripRatingRepository struct {
	db *gorm.DB
}

// NewTripRatingRepository constructs a GORM-backed TripRatingRepository.
func NewTripRatingRepository(db *gorm.DB) TripRatingRepository {
	return &tripRatingRepository{db: db}
}

func (r *tripRatingRepository) Create(ctx context.Context, rating *models.TripRating) error {
	return r.db.WithContext(ctx).Create(rating).Error
}

func (r *tripRatingRepository) FindByTripAndRole(ctx context.Context, tripID string, role models.RaterRole) (*models.TripRating, error) {
	var rating models.TripRating
	err := r.db.WithContext(ctx).
		Where("trip_id = ? AND rater_role = ?", tripID, role).
		First(&rating).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &rating, nil
}
