package repository

import (
	"context"
	"errors"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"ridematch-backend/internal/models"
)

// ---------------- payout accounts ----------------

// PayoutAccountRepository persists drivers' bank accounts.
type PayoutAccountRepository interface {
	// Upsert creates the account or, if the same bank account already
	// exists for the user (e.g. previously removed), replaces its status
	// and resolved name.
	Upsert(ctx context.Context, a *models.PayoutAccount) (*models.PayoutAccount, error)
	FindByID(ctx context.Context, id string) (*models.PayoutAccount, error)
	ListByUser(ctx context.Context, userID string) ([]models.PayoutAccount, error)
	ListByStatus(ctx context.Context, status string, limit, offset int) ([]models.PayoutAccount, int64, error)
	Update(ctx context.Context, a *models.PayoutAccount) error
}

type payoutAccountRepository struct{ db *gorm.DB }

func NewPayoutAccountRepository(db *gorm.DB) PayoutAccountRepository {
	return &payoutAccountRepository{db: db}
}

func (r *payoutAccountRepository) Upsert(ctx context.Context, a *models.PayoutAccount) (*models.PayoutAccount, error) {
	var existing models.PayoutAccount
	err := r.db.WithContext(ctx).
		First(&existing, "user_id = ? AND bank_code = ? AND account_number = ?", a.UserID, a.BankCode, a.AccountNumber).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		if err := r.db.WithContext(ctx).Create(a).Error; err != nil {
			return nil, err
		}
		return a, nil
	}
	if err != nil {
		return nil, err
	}
	existing.BankName = a.BankName
	existing.AccountName = a.AccountName
	existing.Status = a.Status
	existing.NameMatchNote = a.NameMatchNote
	existing.ReviewedBy, existing.ReviewedAt = "", nil
	if err := r.db.WithContext(ctx).Save(&existing).Error; err != nil {
		return nil, err
	}
	return &existing, nil
}

func (r *payoutAccountRepository) FindByID(ctx context.Context, id string) (*models.PayoutAccount, error) {
	var a models.PayoutAccount
	err := r.db.WithContext(ctx).First(&a, "id = ?", id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	return &a, err
}

func (r *payoutAccountRepository) ListByUser(ctx context.Context, userID string) ([]models.PayoutAccount, error) {
	var rows []models.PayoutAccount
	err := r.db.WithContext(ctx).Where("user_id = ? AND status <> ?", userID, models.PayoutAccountRemoved).
		Order("created_at DESC").Find(&rows).Error
	return rows, err
}

func (r *payoutAccountRepository) ListByStatus(ctx context.Context, status string, limit, offset int) ([]models.PayoutAccount, int64, error) {
	q := r.db.WithContext(ctx).Model(&models.PayoutAccount{})
	if status != "" {
		q = q.Where("status = ?", status)
	}
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var rows []models.PayoutAccount
	err := q.Order("created_at DESC").Limit(limit).Offset(offset).Find(&rows).Error
	return rows, total, err
}

func (r *payoutAccountRepository) Update(ctx context.Context, a *models.PayoutAccount) error {
	return r.db.WithContext(ctx).Save(a).Error
}

// ---------------- next of kin & appeals ----------------

// KinRepository persists next-of-kin records and withdrawal appeals.
type KinRepository interface {
	FindByUser(ctx context.Context, userID string) (*models.NextOfKin, error)
	FindByPhone(ctx context.Context, phone string) ([]models.NextOfKin, error)
	Upsert(ctx context.Context, n *models.NextOfKin) error

	CreateAppeal(ctx context.Context, a *models.WithdrawalAppeal) error
	FindAppealByID(ctx context.Context, id string) (*models.WithdrawalAppeal, error)
	FindAppealByTokenHash(ctx context.Context, hash string) (*models.WithdrawalAppeal, error)
	ListAppeals(ctx context.Context, status string, limit, offset int) ([]models.WithdrawalAppeal, int64, error)
	ListOpenAppealsForDriver(ctx context.Context, driverUserID string) ([]models.WithdrawalAppeal, error)
	UpdateAppeal(ctx context.Context, a *models.WithdrawalAppeal) error
	// TransitionAppeal is an atomic compare-and-swap on status.
	TransitionAppeal(ctx context.Context, id string, from []models.AppealStatus, updates map[string]interface{}) (bool, error)
	DueAppeals(ctx context.Context, now time.Time, limit int) ([]models.WithdrawalAppeal, error)

	AddDocument(ctx context.Context, d *models.AppealDocument) error
	ListDocuments(ctx context.Context, appealID string) ([]models.AppealDocument, error)
	CountDocuments(ctx context.Context, appealID string) (int64, error)
}

type kinRepository struct{ db *gorm.DB }

func NewKinRepository(db *gorm.DB) KinRepository { return &kinRepository{db: db} }

func (r *kinRepository) FindByUser(ctx context.Context, userID string) (*models.NextOfKin, error) {
	var n models.NextOfKin
	err := r.db.WithContext(ctx).First(&n, "user_id = ?", userID).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	return &n, err
}

func (r *kinRepository) FindByPhone(ctx context.Context, phone string) ([]models.NextOfKin, error) {
	var rows []models.NextOfKin
	err := r.db.WithContext(ctx).Where("phone = ?", phone).Find(&rows).Error
	return rows, err
}

func (r *kinRepository) Upsert(ctx context.Context, n *models.NextOfKin) error {
	var existing models.NextOfKin
	err := r.db.WithContext(ctx).First(&existing, "user_id = ?", n.UserID).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return r.db.WithContext(ctx).Create(n).Error
	}
	if err != nil {
		return err
	}
	n.ID, n.CreatedAt = existing.ID, existing.CreatedAt
	return r.db.WithContext(ctx).Save(n).Error
}

func (r *kinRepository) CreateAppeal(ctx context.Context, a *models.WithdrawalAppeal) error {
	return r.db.WithContext(ctx).Create(a).Error
}

func (r *kinRepository) FindAppealByID(ctx context.Context, id string) (*models.WithdrawalAppeal, error) {
	var a models.WithdrawalAppeal
	err := r.db.WithContext(ctx).First(&a, "id = ?", id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	return &a, err
}

func (r *kinRepository) FindAppealByTokenHash(ctx context.Context, hash string) (*models.WithdrawalAppeal, error) {
	var a models.WithdrawalAppeal
	err := r.db.WithContext(ctx).First(&a, "token_hash = ?", hash).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	return &a, err
}

func (r *kinRepository) ListAppeals(ctx context.Context, status string, limit, offset int) ([]models.WithdrawalAppeal, int64, error) {
	q := r.db.WithContext(ctx).Model(&models.WithdrawalAppeal{})
	if status != "" {
		q = q.Where("status = ?", status)
	}
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var rows []models.WithdrawalAppeal
	err := q.Order("created_at DESC").Limit(limit).Offset(offset).Find(&rows).Error
	return rows, total, err
}

var openAppealStatuses = []models.AppealStatus{
	models.AppealSubmitted, models.AppealInfoRequested, models.AppealRecommended, models.AppealApproved,
}

func (r *kinRepository) ListOpenAppealsForDriver(ctx context.Context, driverUserID string) ([]models.WithdrawalAppeal, error) {
	var rows []models.WithdrawalAppeal
	err := r.db.WithContext(ctx).Where("driver_user_id = ? AND status IN ?", driverUserID, openAppealStatuses).
		Order("created_at DESC").Find(&rows).Error
	return rows, err
}

func (r *kinRepository) UpdateAppeal(ctx context.Context, a *models.WithdrawalAppeal) error {
	return r.db.WithContext(ctx).Save(a).Error
}

func (r *kinRepository) TransitionAppeal(ctx context.Context, id string, from []models.AppealStatus, updates map[string]interface{}) (bool, error) {
	res := r.db.WithContext(ctx).Model(&models.WithdrawalAppeal{}).
		Where("id = ? AND status IN ?", id, from).Updates(updates)
	return res.RowsAffected > 0, res.Error
}

func (r *kinRepository) DueAppeals(ctx context.Context, now time.Time, limit int) ([]models.WithdrawalAppeal, error) {
	var rows []models.WithdrawalAppeal
	err := r.db.WithContext(ctx).
		Where("status = ? AND disbursable_after IS NOT NULL AND disbursable_after <= ?", models.AppealApproved, now).
		Limit(limit).Find(&rows).Error
	return rows, err
}

func (r *kinRepository) AddDocument(ctx context.Context, d *models.AppealDocument) error {
	return r.db.WithContext(ctx).Create(d).Error
}

func (r *kinRepository) ListDocuments(ctx context.Context, appealID string) ([]models.AppealDocument, error) {
	var rows []models.AppealDocument
	err := r.db.WithContext(ctx).Where("appeal_id = ?", appealID).Order("created_at ASC").Find(&rows).Error
	return rows, err
}

func (r *kinRepository) CountDocuments(ctx context.Context, appealID string) (int64, error) {
	var n int64
	err := r.db.WithContext(ctx).Model(&models.AppealDocument{}).Where("appeal_id = ?", appealID).Count(&n).Error
	return n, err
}

// ---------------- audit log & platform settings ----------------

// AuditRepository appends to and reads the audit log.
type AuditRepository interface {
	Append(ctx context.Context, e *models.AuditLog) error
	List(ctx context.Context, action, targetType, targetID string, limit, offset int) ([]models.AuditLog, int64, error)
}

type auditRepository struct{ db *gorm.DB }

func NewAuditRepository(db *gorm.DB) AuditRepository { return &auditRepository{db: db} }

func (r *auditRepository) Append(ctx context.Context, e *models.AuditLog) error {
	if e.CreatedAt.IsZero() {
		e.CreatedAt = time.Now()
	}
	return r.db.WithContext(ctx).Create(e).Error
}

func (r *auditRepository) List(ctx context.Context, action, targetType, targetID string, limit, offset int) ([]models.AuditLog, int64, error) {
	q := r.db.WithContext(ctx).Model(&models.AuditLog{})
	if action != "" {
		q = q.Where("action = ?", action)
	}
	if targetType != "" {
		q = q.Where("target_type = ?", targetType)
	}
	if targetID != "" {
		q = q.Where("target_id = ?", targetID)
	}
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var rows []models.AuditLog
	err := q.Order("id DESC").Limit(limit).Offset(offset).Find(&rows).Error
	return rows, total, err
}

// PlatformSettingRepository stores runtime switches.
type PlatformSettingRepository interface {
	Get(ctx context.Context, key string) (string, bool, error)
	Set(ctx context.Context, key, value, updatedBy string) error
}

type platformSettingRepository struct{ db *gorm.DB }

func NewPlatformSettingRepository(db *gorm.DB) PlatformSettingRepository {
	return &platformSettingRepository{db: db}
}

func (r *platformSettingRepository) Get(ctx context.Context, key string) (string, bool, error) {
	var s models.PlatformSetting
	err := r.db.WithContext(ctx).Where(clause.Eq{Column: clause.Column{Name: "key"}, Value: key}).First(&s).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return s.Value, true, nil
}

func (r *platformSettingRepository) Set(ctx context.Context, key, value, updatedBy string) error {
	s := models.PlatformSetting{Key: key, Value: value, UpdatedBy: updatedBy, UpdatedAt: time.Now()}
	return r.db.WithContext(ctx).Save(&s).Error
}
