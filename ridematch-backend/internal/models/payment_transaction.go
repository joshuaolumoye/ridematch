package models

import "time"

// PaymentStatus tracks a payment transaction through its lifecycle, from
// checkout-link creation to Flutterwave confirming (or failing) the charge.
type PaymentStatus string

const (
	PaymentInitiated  PaymentStatus = "initiated"  // checkout link created, awaiting the driver to pay
	PaymentSuccessful PaymentStatus = "successful" // confirmed via webhook + server-side verification
	PaymentFailed     PaymentStatus = "failed"     // Flutterwave reported a failed/abandoned charge
)

// PaymentPurpose distinguishes what a payment was for. Only driver
// subscription exists today, but the type exists so future payment flows
// (e.g. a one-off promo, an in-app top-up) don't need a schema change.
type PaymentPurpose string

const (
	PurposeDriverSubscription PaymentPurpose = "driver_subscription"
)

// PaymentTransaction is our own authoritative record of a payment attempt,
// created the moment a checkout link is generated — before Flutterwave has
// confirmed anything. This is what makes the webhook handler idempotent
// and safe to verify against: it looks up this row by TxRef (a reference
// *we* generated), not by trusting whatever the webhook body claims, and
// the DaysRequested/AmountKobo it grants come from this row, not from the
// webhook payload.
type PaymentTransaction struct {
	Base

	DriverID string         `gorm:"type:char(36);not null;index" json:"driver_id"`
	TxRef    string         `gorm:"type:varchar(100);uniqueIndex;not null" json:"tx_ref"`
	Purpose  PaymentPurpose `gorm:"type:varchar(50);not null;default:'driver_subscription'" json:"purpose"`

	AmountKobo    int64  `gorm:"not null" json:"amount_kobo"`
	Currency      string `gorm:"type:varchar(10);not null;default:'NGN'" json:"currency"`
	DaysRequested int    `gorm:"not null" json:"days_requested"`

	Status           PaymentStatus `gorm:"type:varchar(20);not null;default:'initiated'" json:"status"`
	FlwTransactionID string        `gorm:"type:varchar(50)" json:"flw_transaction_id,omitempty"`
	FailureReason    string        `gorm:"type:varchar(255)" json:"failure_reason,omitempty"`
	CompletedAt      *time.Time    `json:"completed_at,omitempty"`

	Driver *DriverProfile `gorm:"foreignKey:DriverID" json:"-"`
}

func (PaymentTransaction) TableName() string {
	return "payment_transactions"
}

// IsTerminal reports whether this transaction has reached a final state
// (successful or failed) and should never be mutated again.
func (p *PaymentTransaction) IsTerminal() bool {
	return p.Status == PaymentSuccessful || p.Status == PaymentFailed
}
