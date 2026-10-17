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

// UserFilter narrows an admin user listing. Zero-value fields are ignored.
type UserFilter struct {
	Role   string // "user" | "driver" | "admin"
	Status string // "active" | "suspended" | "banned"
	// Query matches name, phone, or email (case-insensitive substring).
	Query string
}

// UserRepository defines persistence operations for User accounts. A user
// authenticates with exactly one of Phone or Email (see models.User), so
// every identifier-based lookup comes in a phone and an email variant.
type UserRepository interface {
	Create(ctx context.Context, user *models.User) error
	FindByID(ctx context.Context, id string) (*models.User, error)
	FindByPhone(ctx context.Context, phone string) (*models.User, error)
	ExistsByPhone(ctx context.Context, phone string) (bool, error)
	FindByEmail(ctx context.Context, email string) (*models.User, error)
	ExistsByEmail(ctx context.Context, email string) (bool, error)
	Update(ctx context.Context, user *models.User) error

	// FindAll returns a filtered, paginated page of users (newest first)
	// plus the total matching count, for the admin users list.
	FindAll(ctx context.Context, filter UserFilter, limit, offset int) ([]models.User, int64, error)

	// Count returns the total number of accounts, ignoring soft-deletes.
	Count(ctx context.Context) (int64, error)

	// CountByStatus groups accounts by UserStatus, for the dashboard's
	// "suspended accounts" stat.
	CountByStatus(ctx context.Context) (map[string]int64, error)

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
		First(&user, "phone = ? AND phone != ''", phone).Error
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
		Where("phone = ? AND phone != ''", phone).
		Count(&count).Error
	return count > 0, err
}

func (r *userRepository) FindByEmail(ctx context.Context, email string) (*models.User, error) {
	var user models.User
	err := r.db.WithContext(ctx).
		Preload("DriverProfile").
		First(&user, "email = ? AND email != ''", email).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &user, nil
}

func (r *userRepository) ExistsByEmail(ctx context.Context, email string) (bool, error) {
	var count int64
	err := r.db.WithContext(ctx).Model(&models.User{}).
		Where("email = ? AND email != ''", email).
		Count(&count).Error
	return count > 0, err
}

func (r *userRepository) Update(ctx context.Context, user *models.User) error {
	return r.db.WithContext(ctx).Save(user).Error
}

func (r *userRepository) Delete(ctx context.Context, id string) error {
	return r.db.WithContext(ctx).Delete(&models.User{}, "id = ?", id).Error
}

func (r *userRepository) filtered(ctx context.Context, filter UserFilter) *gorm.DB {
	q := r.db.WithContext(ctx).Model(&models.User{})

	if filter.Role != "" {
		q = q.Where("role = ?", filter.Role)
	}
	if filter.Status != "" {
		q = q.Where("status = ?", filter.Status)
	}
	if filter.Query != "" {
		like := "%" + filter.Query + "%"
		q = q.Where("name LIKE ? OR phone LIKE ? OR email LIKE ?", like, like, like)
	}
	return q
}

func (r *userRepository) FindAll(ctx context.Context, filter UserFilter, limit, offset int) ([]models.User, int64, error) {
	var total int64
	if err := r.filtered(ctx, filter).Count(&total).Error; err != nil {
		return nil, 0, err
	}

	var users []models.User
	err := r.filtered(ctx, filter).
		Preload("DriverProfile").
		Order("created_at DESC").
		Limit(limit).
		Offset(offset).
		Find(&users).Error
	if err != nil {
		return nil, 0, err
	}
	return users, total, nil
}

func (r *userRepository) Count(ctx context.Context) (int64, error) {
	var count int64
	err := r.db.WithContext(ctx).Model(&models.User{}).Count(&count).Error
	return count, err
}

func (r *userRepository) CountByStatus(ctx context.Context) (map[string]int64, error) {
	var rows []struct {
		Status string
		Count  int64
	}
	err := r.db.WithContext(ctx).Model(&models.User{}).
		Select("status, COUNT(*) AS count").
		Group("status").
		Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	result := make(map[string]int64, len(rows))
	for _, row := range rows {
		result[row.Status] = row.Count
	}
	return result, nil
}
