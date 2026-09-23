package repository

import (
	"context"
	"errors"

	"gorm.io/gorm"

	"ridematch-backend/internal/models"
)

// PaymentRepository defines persistence operations for payment
// transactions (currently: driver subscription checkouts).
type PaymentRepository interface {
	Create(ctx context.Context, tx *models.PaymentTransaction) error
	FindByTxRef(ctx context.Context, txRef string) (*models.PaymentTransaction, error)
	Update(ctx context.Context, tx *models.PaymentTransaction) error
}

type paymentRepository struct {
	db *gorm.DB
}

// NewPaymentRepository constructs a GORM-backed PaymentRepository.
func NewPaymentRepository(db *gorm.DB) PaymentRepository {
	return &paymentRepository{db: db}
}

func (r *paymentRepository) Create(ctx context.Context, tx *models.PaymentTransaction) error {
	return r.db.WithContext(ctx).Create(tx).Error
}

func (r *paymentRepository) FindByTxRef(ctx context.Context, txRef string) (*models.PaymentTransaction, error) {
	var tx models.PaymentTransaction
	err := r.db.WithContext(ctx).First(&tx, "tx_ref = ?", txRef).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &tx, nil
}

func (r *paymentRepository) Update(ctx context.Context, tx *models.PaymentTransaction) error {
	return r.db.WithContext(ctx).Save(tx).Error
}
