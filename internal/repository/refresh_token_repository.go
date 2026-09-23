package repository

import (
	"context"
	"errors"
	"time"

	"gorm.io/gorm"

	"ridematch-backend/internal/models"
)

// RefreshTokenRepository defines persistence operations for refresh
// tokens, used to support logout and token rotation.
type RefreshTokenRepository interface {
	Create(ctx context.Context, token *models.RefreshToken) error
	FindByHash(ctx context.Context, hash string) (*models.RefreshToken, error)
	Revoke(ctx context.Context, hash string) error
	RevokeAllForUser(ctx context.Context, userID string) error
	DeleteExpired(ctx context.Context, olderThan time.Time) error
}

type refreshTokenRepository struct {
	db *gorm.DB
}

// NewRefreshTokenRepository constructs a GORM-backed RefreshTokenRepository.
func NewRefreshTokenRepository(db *gorm.DB) RefreshTokenRepository {
	return &refreshTokenRepository{db: db}
}

func (r *refreshTokenRepository) Create(ctx context.Context, token *models.RefreshToken) error {
	return r.db.WithContext(ctx).Create(token).Error
}

func (r *refreshTokenRepository) FindByHash(ctx context.Context, hash string) (*models.RefreshToken, error) {
	var token models.RefreshToken
	err := r.db.WithContext(ctx).First(&token, "token_hash = ?", hash).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &token, nil
}

func (r *refreshTokenRepository) Revoke(ctx context.Context, hash string) error {
	return r.db.WithContext(ctx).
		Model(&models.RefreshToken{}).
		Where("token_hash = ?", hash).
		Update("revoked_at", time.Now()).Error
}

func (r *refreshTokenRepository) RevokeAllForUser(ctx context.Context, userID string) error {
	return r.db.WithContext(ctx).
		Model(&models.RefreshToken{}).
		Where("user_id = ? AND revoked_at IS NULL", userID).
		Update("revoked_at", time.Now()).Error
}

func (r *refreshTokenRepository) DeleteExpired(ctx context.Context, olderThan time.Time) error {
	return r.db.WithContext(ctx).
		Unscoped().
		Where("expires_at < ?", olderThan).
		Delete(&models.RefreshToken{}).Error
}
