package dto

import "time"

// WalletLimits are the server-side limits the app should display. The server
// enforces them; the app never needs to hardcode any of these.
type WalletLimits struct {
	TopupMinKobo        int64 `json:"topup_min_kobo"`
	TopupMaxKobo        int64 `json:"topup_max_kobo"`
	TopupFeeBps         int   `json:"topup_fee_bps"`
	WithdrawMinKobo     int64 `json:"withdraw_min_kobo"`
	DailyLimitKobo      int64 `json:"daily_limit_kobo"`
	CooldownCapKobo     int64 `json:"cooldown_cap_kobo"`
	ReviewThresholdKobo int64 `json:"review_threshold_kobo"`
}

// WalletResponse is the caller's wallet overview.
type WalletResponse struct {
	BalanceKobo           int64        `json:"balance_kobo"`            // spendable / withdrawable now
	HeldForTripsKobo      int64        `json:"held_for_trips_kobo"`     // passenger side: fares in escrow
	IncomingKobo          int64        `json:"incoming_kobo"`           // driver side: fares in escrow awaiting release
	PendingWithdrawalKobo int64        `json:"pending_withdrawal_kobo"` // withdrawals not yet confirmed by the bank
	Currency              string       `json:"currency"`
	IsDriver              bool         `json:"is_driver"`
	CanWithdraw           bool         `json:"can_withdraw"` // driver, payouts on, wallet not frozen
	PINSet                bool         `json:"pin_set"`
	PINLockedUntil        *time.Time   `json:"pin_locked_until,omitempty"`
	CooldownUntil         *time.Time   `json:"cooldown_until,omitempty"`
	WithdrawalsFrozen     bool         `json:"withdrawals_frozen"`
	FrozenReason          string       `json:"frozen_reason,omitempty"`
	PayoutsEnabled        bool         `json:"payouts_enabled"`
	Limits                WalletLimits `json:"limits"`
}

// StatementEntryResponse is one line of the wallet history.
type StatementEntryResponse struct {
	ID           uint64    `json:"id"`
	Kind         string    `json:"kind"`
	Title        string    `json:"title"`
	AmountKobo   int64     `json:"amount_kobo"` // signed: + money in, - money out
	BalanceAfter *int64    `json:"balance_after_kobo,omitempty"`
	RefType      string    `json:"ref_type,omitempty"`
	RefID        string    `json:"ref_id,omitempty"`
	CreatedAt    time.Time `json:"created_at"`
}

// TopupQuoteResponse previews what a top-up costs.
type TopupQuoteResponse struct {
	RequestedKobo int64 `json:"requested_kobo"`
	FeeKobo       int64 `json:"fee_kobo"`
	GrossKobo     int64 `json:"gross_kobo"` // what the payer transfers
	FeeBps        int   `json:"fee_bps"`
}

// CreateTopupRequest asks for a one-time account number.
type CreateTopupRequest struct {
	AmountKobo int64 `json:"amount_kobo" binding:"required,min=1"` // what should land in the wallet
}

// TopupResponse is a top-up request and, while pending, the account to pay.
type TopupResponse struct {
	ID            string     `json:"id"`
	Status        string     `json:"status"`
	RequestedKobo int64      `json:"requested_kobo"`
	FeeKobo       int64      `json:"fee_kobo"`
	GrossKobo     int64      `json:"gross_kobo"`
	AccountNumber string     `json:"account_number,omitempty"`
	BankName      string     `json:"bank_name,omitempty"`
	ExpiresAt     *time.Time `json:"expires_at,omitempty"`
	CreditedKobo  int64      `json:"credited_kobo"`
	CompletedAt   *time.Time `json:"completed_at,omitempty"`
	CreatedAt     time.Time  `json:"created_at"`
}

// BankResponse is one bank in the picker.
type BankResponse struct {
	Code string `json:"code"`
	Name string `json:"name"`
}

// ResolveAccountRequest looks up an account holder's name.
type ResolveAccountRequest struct {
	BankCode      string `json:"bank_code" binding:"required"`
	AccountNumber string `json:"account_number" binding:"required,len=10,numeric"`
}

// ResolveAccountResponse is the name the bank holds for the account.
type ResolveAccountResponse struct {
	AccountName string `json:"account_name"`
}

// AddPayoutAccountRequest saves a bank account (requires a security code).
type AddPayoutAccountRequest struct {
	BankCode      string `json:"bank_code" binding:"required"`
	AccountNumber string `json:"account_number" binding:"required,len=10,numeric"`
	OTP           string `json:"otp" binding:"required"`
}

// PayoutAccountResponse is a saved bank account.
type PayoutAccountResponse struct {
	ID            string    `json:"id"`
	BankCode      string    `json:"bank_code"`
	BankName      string    `json:"bank_name"`
	AccountNumber string    `json:"account_number"` // masked for the owner
	AccountName   string    `json:"account_name"`
	Status        string    `json:"status"`
	StatusNote    string    `json:"status_note,omitempty"`
	CreatedAt     time.Time `json:"created_at"`
}

// SetPINRequest sets or resets the withdrawal PIN (requires a security code).
type SetPINRequest struct {
	PIN string `json:"pin" binding:"required"`
	OTP string `json:"otp" binding:"required"`
}

// RemovePayoutAccountRequest confirms removal with the PIN.
type RemovePayoutAccountRequest struct {
	PIN string `json:"pin" binding:"required"`
}

// WithdrawRequest asks to pay out to a saved account.
type WithdrawRequest struct {
	PayoutAccountID string `json:"payout_account_id" binding:"required"`
	AmountKobo      int64  `json:"amount_kobo" binding:"required,min=1"`
	PIN             string `json:"pin" binding:"required"`
}

// WithdrawQuoteResponse previews a withdrawal.
type WithdrawQuoteResponse struct {
	AmountKobo int64 `json:"amount_kobo"`
	FeeKobo    int64 `json:"fee_kobo"`
	NetKobo    int64 `json:"net_kobo"` // lands in the bank account
}

// WithdrawalResponse is a payout as the driver sees it.
type WithdrawalResponse struct {
	ID            string     `json:"id"`
	Status        string     `json:"status"`
	AmountKobo    int64      `json:"amount_kobo"`
	FeeKobo       int64      `json:"fee_kobo"`
	NetKobo       int64      `json:"net_kobo"`
	BankName      string     `json:"bank_name"`
	AccountNumber string     `json:"account_number"` // masked
	AccountName   string     `json:"account_name"`
	StatusNote    string     `json:"status_note,omitempty"`
	CreatedAt     time.Time  `json:"created_at"`
	CompletedAt   *time.Time `json:"completed_at,omitempty"`
}

// EscrowResponse describes the payment held for a trip.
type EscrowResponse struct {
	TripID       string     `json:"trip_id"`
	Status       string     `json:"status"`
	AmountKobo   int64      `json:"amount_kobo"`
	ReleaseAfter *time.Time `json:"release_after,omitempty"`
	Disputed     bool       `json:"disputed"`
}

// DisputeTripRequest reports a problem with a completed trip.
type DisputeTripRequest struct {
	Reason string `json:"reason" binding:"required,min=5,max=500"`
}

// ---- admin ----

// AdminWithdrawalResponse is a payout with everything an admin needs.
type AdminWithdrawalResponse struct {
	ID            string     `json:"id"`
	UserID        string     `json:"user_id"`
	DriverName    string     `json:"driver_name"`
	DriverPhone   string     `json:"driver_phone,omitempty"`
	Kind          string     `json:"kind"`
	Status        string     `json:"status"`
	AmountKobo    int64      `json:"amount_kobo"`
	FeeKobo       int64      `json:"fee_kobo"`
	NetKobo       int64      `json:"net_kobo"`
	BankName      string     `json:"bank_name"`
	AccountNumber string     `json:"account_number"`
	AccountName   string     `json:"account_name"`
	Reference     string     `json:"reference"`
	FlwTransferID string     `json:"flw_transfer_id,omitempty"`
	FailureReason string     `json:"failure_reason,omitempty"`
	RiskFlags     string     `json:"risk_flags,omitempty"`
	AppealID      *string    `json:"appeal_id,omitempty"`
	ReviewNote    string     `json:"review_note,omitempty"`
	ReviewedBy    string     `json:"reviewed_by,omitempty"`
	AttemptCount  int        `json:"attempt_count"`
	CreatedAt     time.Time  `json:"created_at"`
	CompletedAt   *time.Time `json:"completed_at,omitempty"`
}

// AdminPayoutAccountResponse is a bank account awaiting/after review.
type AdminPayoutAccountResponse struct {
	ID             string    `json:"id"`
	UserID         string    `json:"user_id"`
	DriverName     string    `json:"driver_name"`
	RegisteredName string    `json:"registered_name"`
	BankName       string    `json:"bank_name"`
	AccountNumber  string    `json:"account_number"`
	AccountName    string    `json:"account_name"`
	Status         string    `json:"status"`
	NameMatchNote  string    `json:"name_match_note,omitempty"`
	CreatedAt      time.Time `json:"created_at"`
}

// AdminEscrowResponse is an escrow hold in the admin list.
type AdminEscrowResponse struct {
	ID            string     `json:"id"`
	TripID        string     `json:"trip_id"`
	PassengerName string     `json:"passenger_name"`
	DriverName    string     `json:"driver_name"`
	AmountKobo    int64      `json:"amount_kobo"`
	Status        string     `json:"status"`
	ReleaseAfter  *time.Time `json:"release_after,omitempty"`
	DisputeReason string     `json:"dispute_reason,omitempty"`
	DisputedAt    *time.Time `json:"disputed_at,omitempty"`
	Resolution    string     `json:"resolution,omitempty"`
	CreatedAt     time.Time  `json:"created_at"`
}

// AdminDecisionRequest carries an admin's decision and mandatory note.
type AdminDecisionRequest struct {
	Decision string `json:"decision" binding:"required"` // endpoint-specific values
	Note     string `json:"note" binding:"required,min=3,max=500"`
}

// AdminWalletResponse is one user's wallet as an admin sees it.
type AdminWalletResponse struct {
	WalletResponse
	UserID   string `json:"user_id"`
	UserName string `json:"user_name"`
}

// PayoutSwitchRequest toggles the global payout kill-switch.
type PayoutSwitchRequest struct {
	Enabled bool   `json:"enabled"`
	Reason  string `json:"reason" binding:"required,min=3,max=300"`
}

// FreezeWalletRequest freezes or unfreezes one wallet's withdrawals.
type FreezeWalletRequest struct {
	Frozen bool   `json:"frozen"`
	Reason string `json:"reason" binding:"required,min=3,max=255"`
}
