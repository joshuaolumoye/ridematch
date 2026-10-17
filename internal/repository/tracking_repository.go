package repository

import (
	"context"
	"errors"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"ridematch-backend/internal/models"
)

// ShiftFilter narrows the admin shift list.
type ShiftFilter struct {
	DriverID string
	Status   string
	From     *time.Time
	To       *time.Time
}

// TrackingRepository persists shifts and their GPS trails.
type TrackingRepository interface {
	// StartShift closes any still-active shift of the driver (as replaced)
	// and creates the new one, atomically.
	StartShift(ctx context.Context, s *models.DriverShift) error
	FindActiveShift(ctx context.Context, driverID string) (*models.DriverShift, error)
	FindShift(ctx context.Context, id string) (*models.DriverShift, error)
	// EndShift closes an active shift. Returns false if it was not active.
	EndShift(ctx context.Context, id string, reason models.ShiftEndReason, at time.Time, lat, lng *float64) (bool, error)
	// AppendPoints locks the shift row, lets build derive the new points
	// from its current chain head (mutating the shift's aggregates), then
	// inserts the points and saves the shift — all in one transaction.
	AppendPoints(ctx context.Context, shiftID string, build func(*models.DriverShift) ([]models.LocationPoint, error)) (*models.DriverShift, error)

	ListShifts(ctx context.Context, f ShiftFilter, limit, offset int) ([]models.DriverShift, int64, error)
	ListPoints(ctx context.Context, shiftID string, limit int) ([]models.LocationPoint, error)
	AllPointsBySeq(ctx context.Context, shiftID string, afterSeq int64, limit int) ([]models.LocationPoint, error)
	PointsInWindow(ctx context.Context, driverID string, from, to time.Time, limit int) ([]models.LocationPoint, error)
	StaleActiveShifts(ctx context.Context, before time.Time, limit int) ([]models.DriverShift, error)
	// PurgeBefore deletes points and ended shifts older than the cutoff;
	// returns the number of points removed.
	PurgeBefore(ctx context.Context, cutoff time.Time) (int64, error)
}

type trackingRepository struct{ db *gorm.DB }

// NewTrackingRepository constructs a GORM-backed TrackingRepository.
func NewTrackingRepository(db *gorm.DB) TrackingRepository { return &trackingRepository{db: db} }

func (r *trackingRepository) StartShift(ctx context.Context, s *models.DriverShift) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		now := time.Now()
		if err := tx.Model(&models.DriverShift{}).
			Where("driver_id = ? AND status = ?", s.DriverID, models.ShiftActive).
			Updates(map[string]interface{}{
				"status": models.ShiftEnded, "ended_at": now, "end_reason": string(models.ShiftEndReplaced),
			}).Error; err != nil {
			return err
		}
		s.Status = models.ShiftActive
		return tx.Create(s).Error
	})
}

func (r *trackingRepository) FindActiveShift(ctx context.Context, driverID string) (*models.DriverShift, error) {
	var s models.DriverShift
	err := r.db.WithContext(ctx).
		Where("driver_id = ? AND status = ?", driverID, models.ShiftActive).
		Order("started_at DESC").First(&s).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	return &s, err
}

func (r *trackingRepository) FindShift(ctx context.Context, id string) (*models.DriverShift, error) {
	var s models.DriverShift
	err := r.db.WithContext(ctx).First(&s, "id = ?", id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	return &s, err
}

func (r *trackingRepository) EndShift(ctx context.Context, id string, reason models.ShiftEndReason, at time.Time, lat, lng *float64) (bool, error) {
	updates := map[string]interface{}{
		"status": models.ShiftEnded, "ended_at": at, "end_reason": string(reason),
	}
	if lat != nil && lng != nil {
		updates["end_lat"], updates["end_lng"] = *lat, *lng
	}
	res := r.db.WithContext(ctx).Model(&models.DriverShift{}).
		Where("id = ? AND status = ?", id, models.ShiftActive).Updates(updates)
	return res.RowsAffected > 0, res.Error
}

func (r *trackingRepository) AppendPoints(ctx context.Context, shiftID string, build func(*models.DriverShift) ([]models.LocationPoint, error)) (*models.DriverShift, error) {
	var out models.DriverShift
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&out, "id = ?", shiftID).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrNotFound
			}
			return err
		}
		pts, err := build(&out)
		if err != nil {
			return err
		}
		if len(pts) > 0 {
			if err := tx.CreateInBatches(&pts, 100).Error; err != nil {
				return err
			}
		}
		return tx.Save(&out).Error
	})
	if err != nil {
		return nil, err
	}
	return &out, nil
}

func (r *trackingRepository) ListShifts(ctx context.Context, f ShiftFilter, limit, offset int) ([]models.DriverShift, int64, error) {
	q := r.db.WithContext(ctx).Model(&models.DriverShift{})
	if f.DriverID != "" {
		q = q.Where("driver_id = ?", f.DriverID)
	}
	if f.Status != "" {
		q = q.Where("status = ?", f.Status)
	}
	if f.From != nil {
		q = q.Where("started_at >= ?", *f.From)
	}
	if f.To != nil {
		q = q.Where("started_at <= ?", *f.To)
	}
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var rows []models.DriverShift
	err := q.Order("started_at DESC").Limit(limit).Offset(offset).Find(&rows).Error
	return rows, total, err
}

func (r *trackingRepository) ListPoints(ctx context.Context, shiftID string, limit int) ([]models.LocationPoint, error) {
	var rows []models.LocationPoint
	err := r.db.WithContext(ctx).Where("shift_id = ?", shiftID).Order("recorded_at ASC, seq ASC").Limit(limit).Find(&rows).Error
	return rows, err
}

func (r *trackingRepository) AllPointsBySeq(ctx context.Context, shiftID string, afterSeq int64, limit int) ([]models.LocationPoint, error) {
	var rows []models.LocationPoint
	err := r.db.WithContext(ctx).Where("shift_id = ? AND seq > ?", shiftID, afterSeq).Order("seq ASC").Limit(limit).Find(&rows).Error
	return rows, err
}

func (r *trackingRepository) PointsInWindow(ctx context.Context, driverID string, from, to time.Time, limit int) ([]models.LocationPoint, error) {
	var rows []models.LocationPoint
	err := r.db.WithContext(ctx).
		Where("driver_id = ? AND recorded_at >= ? AND recorded_at <= ?", driverID, from, to).
		Order("recorded_at ASC, seq ASC").Limit(limit).Find(&rows).Error
	return rows, err
}

func (r *trackingRepository) StaleActiveShifts(ctx context.Context, before time.Time, limit int) ([]models.DriverShift, error) {
	var rows []models.DriverShift
	err := r.db.WithContext(ctx).
		Where("status = ? AND COALESCE(last_point_at, started_at) < ?", models.ShiftActive, before).
		Limit(limit).Find(&rows).Error
	return rows, err
}

func (r *trackingRepository) PurgeBefore(ctx context.Context, cutoff time.Time) (int64, error) {
	var deleted int64
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		sub := tx.Model(&models.DriverShift{}).Select("id").Where("status = ? AND ended_at < ?", models.ShiftEnded, cutoff)
		res := tx.Where("shift_id IN (?)", sub).Delete(&models.LocationPoint{})
		if res.Error != nil {
			return res.Error
		}
		deleted = res.RowsAffected
		return tx.Where("status = ? AND ended_at < ?", models.ShiftEnded, cutoff).Delete(&models.DriverShift{}).Error
	})
	return deleted, err
}
