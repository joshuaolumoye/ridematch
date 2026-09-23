// Package repository implements the repository pattern: every table is
// accessed only through a narrow interface, so the service layer never
// imports GORM directly and swapping the persistence engine later only
// touches this package.
package repository

import (
	"context"
	"errors"

	"gorm.io/gorm"

	"ridematch-backend/internal/models"
)

// ErrNotFound is returned by any repository method that looked up a single
// record that doesn't exist. Callers check for this with errors.Is rather
// than depending on gorm.ErrRecordNotFound directly.
var ErrNotFound = errors.New("repository: record not found")

// UserRepository defines persistence operations for User accounts.
type UserRepository interface {
	Create(ctx context.Context, user *models.User) error
	FindByID(ctx context.Context, id string) (*models.User, error)
	FindByPhone(ctx context.Context, phone string) (*models.User, error)
	ExistsByPhone(ctx context.Context, phone string) (bool, error)
	Update(ctx context.Context, user *models.User) error

	// Delete soft-deletes the user (GORM sets deleted_at; the row and its
	// history — trips, ratings — are kept for the other party's records,
	// but the account no longer authenticates and is excluded from every
	// normal query). Callers should scrub identifying fields via Update
	// first, since a soft-deleted row otherwise still holds them.
	Delete(ctx context.Context, id string) error
}

type userRepository struct {
	db *gorm.DB
}

// NewUserRepository constructs a GORM-backed UserRepository.
func NewUserRepository(db *gorm.DB) UserRepository {
	return &userRepository{db: db}
}

func (r *userRepository) Create(ctx context.Context, user *models.User) error {
	return r.db.WithContext(ctx).Create(user).Error
}

func (r *userRepository) FindByID(ctx context.Context, id string) (*models.User, error) {
	var user models.User
	err := r.db.WithContext(ctx).
		Preload("DriverProfile").
		First(&user, "id = ?", id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &user, nil
}

func (r *userRepository) FindByPhone(ctx context.Context, phone string) (*models.User, error) {
	var user models.User
	err := r.db.WithContext(ctx).
		Preload("DriverProfile").
		First(&user, "phone = ?", phone).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &user, nil
}

func (r *userRepository) ExistsByPhone(ctx context.Context, phone string) (bool, error) {
	var count int64
	err := r.db.WithContext(ctx).Model(&models.User{}).
		Where("phone = ?", phone).
		Count(&count).Error
	return count > 0, err
}

func (r *userRepository) Update(ctx context.Context, user *models.User) error {
	return r.db.WithContext(ctx).Save(user).Error
}

func (r *userRepository) Delete(ctx context.Context, id string) error {
	return r.db.WithContext(ctx).Delete(&models.User{}, "id = ?", id).Error
}
