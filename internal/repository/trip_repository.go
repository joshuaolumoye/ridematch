package repository

import (
	"context"
	"errors"
	"fmt"
	"time"

	"gorm.io/gorm"

	"ridematch-backend/internal/models"
)

// TripRepository defines persistence operations for Trip records.
type TripRepository interface {
	Create(ctx context.Context, trip *models.Trip) error
	FindByID(ctx context.Context, id string) (*models.Trip, error)
	Update(ctx context.Context, trip *models.Trip) error

	// FindActiveByPassenger returns the passenger's current in-progress
	// trip (requested, matched, or picked_up), if any. Used to enforce
	// one active trip per passenger at a time.
	FindActiveByPassenger(ctx context.Context, passengerID string) (*models.Trip, error)

	// FindActiveByDriver returns the driver's current matched/picked_up
	// trip, if any. Used to enforce one active trip per driver at a time.
	FindActiveByDriver(ctx context.Context, driverID string) (*models.Trip, error)

	// FindHistoryByPassenger returns a page of the passenger's trips
	// (every status, newest first) plus the total count for pagination.
	FindHistoryByPassenger(ctx context.Context, passengerID string, limit, offset int) ([]models.Trip, int64, error)

	// FindHistoryByDriver returns a page of the driver's trips (every
	// status, newest first) plus the total count for pagination —
	// symmetric to FindHistoryByPassenger.
	FindHistoryByDriver(ctx context.Context, driverProfileID string, limit, offset int) ([]models.Trip, int64, error)

	// SumEarningsByDriver aggregates a driver's completed trips: total
	// kobo earned and trip count, all-time and for the current calendar
	// day (server local time). A real SQL aggregate rather than summing
	// a paginated list client-side, so it stays correct regardless of
	// how many trips the driver has completed.
	SumEarningsByDriver(ctx context.Context, driverProfileID string) (totalKobo int64, totalTrips int64, todayKobo int64, todayTrips int64, err error)

	// CompareAndSwapStatus atomically applies `updates` (which must
	// include a "status" key) only if the trip's current status still
	// equals expectedStatus — a single conditional UPDATE, relying on
	// MySQL's own row locking for correctness rather than an
	// application-level lock. Returns false (no error) if the row didn't
	// match, meaning another request already moved the trip on —
	// callers treat that as a normal "state changed under you" failure,
	// not an infrastructure error. This is what makes double-tapping
	// "accept offer" or "confirm pickup" safe under concurrency.
	CompareAndSwapStatus(ctx context.Context, tripID string, expectedStatus models.TripStatus, updates map[string]interface{}) (bool, error)
}

type tripRepository struct {
	db *gorm.DB
}

// NewTripRepository constructs a GORM-backed TripRepository.
func NewTripRepository(db *gorm.DB) TripRepository {
	return &tripRepository{db: db}
}

func (r *tripRepository) Create(ctx context.Context, trip *models.Trip) error {
	return r.db.WithContext(ctx).Create(trip).Error
}

func (r *tripRepository) FindByID(ctx context.Context, id string) (*models.Trip, error) {
	var trip models.Trip
	err := r.db.WithContext(ctx).
		Preload("Passenger").
		Preload("Driver.User").
		First(&trip, "id = ?", id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &trip, nil
}

func (r *tripRepository) Update(ctx context.Context, trip *models.Trip) error {
	return r.db.WithContext(ctx).Save(trip).Error
}

func (r *tripRepository) FindActiveByPassenger(ctx context.Context, passengerID string) (*models.Trip, error) {
	var trip models.Trip
	err := r.db.WithContext(ctx).
		Where("passenger_id = ? AND status IN ?", passengerID, []models.TripStatus{
			models.TripRequested, models.TripMatched, models.TripPickedUp,
		}).
		Order("created_at DESC").
		First(&trip).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &trip, nil
}

func (r *tripRepository) FindHistoryByPassenger(ctx context.Context, passengerID string, limit, offset int) ([]models.Trip, int64, error) {
	var trips []models.Trip
	var total int64

	if err := r.db.WithContext(ctx).
		Model(&models.Trip{}).
		Where("passenger_id = ?", passengerID).
		Count(&total).Error; err != nil {
		return nil, 0, err
	}

	err := r.db.WithContext(ctx).
		Preload("Driver.User").
		Where("passenger_id = ?", passengerID).
		Order("created_at DESC").
		Limit(limit).
		Offset(offset).
		Find(&trips).Error
	if err != nil {
		return nil, 0, err
	}
	return trips, total, nil
}

func (r *tripRepository) FindHistoryByDriver(ctx context.Context, driverProfileID string, limit, offset int) ([]models.Trip, int64, error) {
	var trips []models.Trip
	var total int64

	if err := r.db.WithContext(ctx).
		Model(&models.Trip{}).
		Where("driver_id = ?", driverProfileID).
		Count(&total).Error; err != nil {
		return nil, 0, err
	}

	err := r.db.WithContext(ctx).
		Preload("Passenger").
		Where("driver_id = ?", driverProfileID).
		Order("created_at DESC").
		Limit(limit).
		Offset(offset).
		Find(&trips).Error
	if err != nil {
		return nil, 0, err
	}
	return trips, total, nil
}

func (r *tripRepository) SumEarningsByDriver(ctx context.Context, driverProfileID string) (totalKobo int64, totalTrips int64, todayKobo int64, todayTrips int64, err error) {
	type aggregateRow struct {
		TotalKobo  int64
		TotalTrips int64
	}

	var all aggregateRow
	if err = r.db.WithContext(ctx).
		Model(&models.Trip{}).
		Where("driver_id = ? AND status = ?", driverProfileID, models.TripCompleted).
		Select("COALESCE(SUM(agreed_price_kobo), 0) AS total_kobo, COUNT(*) AS total_trips").
		Scan(&all).Error; err != nil {
		return 0, 0, 0, 0, err
	}

	var today aggregateRow
	now := time.Now()
	year, month, day := now.Date()
	startOfDay := time.Date(year, month, day, 0, 0, 0, 0, now.Location())
	if err = r.db.WithContext(ctx).
		Model(&models.Trip{}).
		Where("driver_id = ? AND status = ? AND completed_at >= ?", driverProfileID, models.TripCompleted, startOfDay).
		Select("COALESCE(SUM(agreed_price_kobo), 0) AS total_kobo, COUNT(*) AS total_trips").
		Scan(&today).Error; err != nil {
		return 0, 0, 0, 0, err
	}

	return all.TotalKobo, all.TotalTrips, today.TotalKobo, today.TotalTrips, nil
}

func (r *tripRepository) CompareAndSwapStatus(ctx context.Context, tripID string, expectedStatus models.TripStatus, updates map[string]interface{}) (bool, error) {
	if _, ok := updates["status"]; !ok {
		return false, fmt.Errorf("repository: CompareAndSwapStatus updates must include a status key")
	}

	result := r.db.WithContext(ctx).
		Model(&models.Trip{}).
		Where("id = ? AND status = ?", tripID, expectedStatus).
		Updates(updates)
	if result.Error != nil {
		return false, result.Error
	}
	return result.RowsAffected > 0, nil
}

func (r *tripRepository) FindActiveByDriver(ctx context.Context, driverID string) (*models.Trip, error) {
	var trip models.Trip
	err := r.db.WithContext(ctx).
		Where("driver_id = ? AND status IN ?", driverID, []models.TripStatus{
			models.TripMatched, models.TripPickedUp,
		}).
		Order("created_at DESC").
		First(&trip).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &trip, nil
}
