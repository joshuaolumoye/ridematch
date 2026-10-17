package repository

import (
	"context"
	"time"

	"gorm.io/gorm"

	"ridematch-backend/internal/models"
)

// NotificationRepository defines persistence operations for a user's
// in-app notification inbox.
type NotificationRepository interface {
	Create(ctx context.Context, notification *models.Notification) error

	// FindByUser returns a page of a user's notifications, newest first,
	// plus the total matching count.
	FindByUser(ctx context.Context, userID string, limit, offset int) ([]models.Notification, int64, error)

	// MarkRead sets ReadAt on one notification, scoped to userID so a
	// user can never mark someone else's notification read. Returns
	// ErrNotFound if no matching, still-unread row exists for that user
	// (already read is treated as a no-op success by the caller, not an
	// error, so this only errors on a genuinely missing/foreign row).
	MarkRead(ctx context.Context, notificationID, userID string) error

	// MarkAllRead sets ReadAt on every unread notification for a user.
	MarkAllRead(ctx context.Context, userID string) error

	// CountUnread returns how many of a user's notifications are unread.
	CountUnread(ctx context.Context, userID string) (int64, error)
}

type notificationRepository struct {
	db *gorm.DB
}

// NewNotificationRepository constructs a GORM-backed NotificationRepository.
func NewNotificationRepository(db *gorm.DB) NotificationRepository {
	return &notificationRepository{db: db}
}

func (r *notificationRepository) Create(ctx context.Context, notification *models.Notification) error {
	return r.db.WithContext(ctx).Create(notification).Error
}

func (r *notificationRepository) FindByUser(ctx context.Context, userID string, limit, offset int) ([]models.Notification, int64, error) {
	var total int64
	if err := r.db.WithContext(ctx).Model(&models.Notification{}).Where("user_id = ?", userID).Count(&total).Error; err != nil {
		return nil, 0, err
	}

	var notifications []models.Notification
	err := r.db.WithContext(ctx).
		Where("user_id = ?", userID).
		Order("created_at DESC").
		Limit(limit).
		Offset(offset).
		Find(&notifications).Error
	if err != nil {
		return nil, 0, err
	}
	return notifications, total, nil
}

func (r *notificationRepository) MarkRead(ctx context.Context, notificationID, userID string) error {
	now := time.Now()
	result := r.db.WithContext(ctx).Model(&models.Notification{}).
		Where("id = ? AND user_id = ? AND read_at IS NULL", notificationID, userID).
		Update("read_at", now)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		// Either it doesn't exist/belong to this user, or it's already
		// read — check which, so an already-read notification isn't
		// reported as an error.
		var exists int64
		if err := r.db.WithContext(ctx).Model(&models.Notification{}).
			Where("id = ? AND user_id = ?", notificationID, userID).
			Count(&exists).Error; err != nil {
			return err
		}
		if exists == 0 {
			return ErrNotFound
		}
	}
	return nil
}

func (r *notificationRepository) MarkAllRead(ctx context.Context, userID string) error {
	now := time.Now()
	return r.db.WithContext(ctx).Model(&models.Notification{}).
		Where("user_id = ? AND read_at IS NULL", userID).
		Update("read_at", now).Error
}

func (r *notificationRepository) CountUnread(ctx context.Context, userID string) (int64, error) {
	var count int64
	err := r.db.WithContext(ctx).Model(&models.Notification{}).
		Where("user_id = ? AND read_at IS NULL", userID).
		Count(&count).Error
	return count, err
}
