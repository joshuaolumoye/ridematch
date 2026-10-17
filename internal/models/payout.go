package models

import "time"

// PayoutAccountStatus is the review state of a driver's bank account.
type PayoutAccountStatus string

const (
	PayoutAccountActive        PayoutAccountStatus = "active"
	PayoutAccountPendingReview PayoutAccountStatus = "pending_review" // account name did not match the driver's name
	PayoutAccountRejected      PayoutAccountStatus = "rejected"
	PayoutAccountRemoved       PayoutAccountStatus = "removed"
)

// PayoutAccount is a bank account a driver may withdraw to. The account
// name comes from the bank (Flutterwave resolve), never from user input.
type PayoutAccount struct {
	ImmutableBase

	UserID        string              `gorm:"type:char(36);not null;uniqueIndex:idx_payout_acct,priority:1" json:"user_id"`
	BankCode      string              `gorm:"type:varchar(20);not null;uniqueIndex:idx_payout_acct,priority:2" json:"bank_code"`
	AccountNumber string              `gorm:"type:varchar(20);not null;uniqueIndex:idx_payout_acct,priority:3" json:"account_number"`
	BankName      string              `gorm:"type:varchar(100);not null" json:"bank_name"`
	AccountName   string              `gorm:"type:varchar(150);not null" json:"account_name"`
	Status        PayoutAccountStatus `gorm:"type:varchar(15);index;not null" json:"status"`
	NameMatchNote string              `gorm:"type:varchar(255)" json:"name_match_note,omitempty"`
	ReviewedBy    string              `gorm:"type:char(36)" json:"reviewed_by,omitempty"`
	ReviewedAt    *time.Time          `json:"reviewed_at,omitempty"`
}

func (PayoutAccount) TableName() string { return "payout_accounts" }

// WithdrawalStatus is the lifecycle of a payout.
type WithdrawalStatus string

const (
	WithdrawalPendingReview  WithdrawalStatus = "pending_review"  // awaiting admin approval (large amount / flagged)
	WithdrawalQueued         WithdrawalStatus = "queued"          // funds locked, transfer not yet sent
	WithdrawalProcessing     WithdrawalStatus = "processing"      // sent to Flutterwave, awaiting outcome
	WithdrawalSuccessful     WithdrawalStatus = "successful"      // bank confirmed
	WithdrawalFailed         WithdrawalStatus = "failed"          // definitively failed; funds returned
	WithdrawalRejected       WithdrawalStatus = "rejected"        // admin declined; funds returned
	WithdrawalNeedsAttention WithdrawalStatus = "needs_attention" // outcome unknown; admin must resolve
)

// WithdrawalKind says who initiated a payout.
type WithdrawalKind string

const (
	WithdrawalKindDriver WithdrawalKind = "driver"
	WithdrawalKindAppeal WithdrawalKind = "appeal"
)

// Withdrawal is one payout. Bank details are snapshotted so later edits to
// the payout account can never redirect or rewrite history.
type Withdrawal struct {
	ImmutableBase

	UserID   string           `gorm:"type:char(36);index;not null" json:"user_id"`
	WalletID string           `gorm:"type:char(36);index;not null" json:"wallet_id"`
	Kind     WithdrawalKind   `gorm:"type:varchar(10);not null" json:"kind"`
	Status   WithdrawalStatus `gorm:"type:varchar(20);index;not null" json:"status"`

	AmountKobo int64 `gorm:"not null" json:"amount_kobo"` // debited from wallet
	FeeKobo    int64 `gorm:"not null" json:"fee_kobo"`    // transfer fee, deducted from amount
	NetKobo    int64 `gorm:"not null" json:"net_kobo"`    // what the bank receives

	PayoutAccountID *string `gorm:"type:char(36)" json:"payout_account_id,omitempty"`
	BankCode        string  `gorm:"type:varchar(20);not null" json:"bank_code"`
	BankName        string  `gorm:"type:varchar(100);not null" json:"bank_name"`
	AccountNumber   string  `gorm:"type:varchar(20);not null" json:"account_number"`
	AccountName     string  `gorm:"type:varchar(150);not null" json:"account_name"`

	Reference     string  `gorm:"type:varchar(100);uniqueIndex;not null" json:"reference"`
	FlwTransferID string  `gorm:"type:varchar(50);index" json:"flw_transfer_id,omitempty"`
	FailureReason string  `gorm:"type:varchar(500)" json:"failure_reason,omitempty"`
	AppealID      *string `gorm:"type:char(36);index" json:"appeal_id,omitempty"`
	RiskFlags     string  `gorm:"type:varchar(255)" json:"risk_flags,omitempty"`
	RequestIP     string  `gorm:"type:varchar(64)" json:"request_ip,omitempty"`
	AttemptCount  int     `json:"attempt_count"`

	ReviewedBy  string     `gorm:"type:char(36)" json:"reviewed_by,omitempty"`
	ReviewedAt  *time.Time `json:"reviewed_at,omitempty"`
	ReviewNote  string     `gorm:"type:varchar(500)" json:"review_note,omitempty"`
	SentAt      *time.Time `json:"sent_at,omitempty"`
	CompletedAt *time.Time `json:"completed_at,omitempty"`
}

func (Withdrawal) TableName() string { return "withdrawals" }

// IsTerminal reports whether the withdrawal has reached a final state.
func (w *Withdrawal) IsTerminal() bool {
	switch w.Status {
	case WithdrawalSuccessful, WithdrawalFailed, WithdrawalRejected:
		return true
	}
	return false
}
