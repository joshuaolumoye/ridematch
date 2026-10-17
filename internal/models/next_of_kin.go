package models

import "time"

// NextOfKin is the person a driver nominates to appeal for their wallet
// funds if the driver dies or becomes unable to act. One per driver.
type NextOfKin struct {
	ImmutableBase

	UserID       string     `gorm:"type:char(36);uniqueIndex;not null" json:"user_id"`
	FullName     string     `gorm:"type:varchar(150);not null" json:"full_name"`
	Relationship string     `gorm:"type:varchar(50);not null" json:"relationship"`
	Phone        string     `gorm:"type:varchar(20);index;not null" json:"phone"` // normalized +234...
	Email        string     `gorm:"type:varchar(255)" json:"email,omitempty"`
	Address      string     `gorm:"type:varchar(255)" json:"address,omitempty"`
	VerifiedAt   *time.Time `json:"verified_at,omitempty"` // driver proved ownership of the change with an OTP
}

func (NextOfKin) TableName() string { return "next_of_kin" }

// AppealStatus is the lifecycle of a next-of-kin withdrawal appeal.
type AppealStatus string

const (
	AppealSubmitted     AppealStatus = "submitted"
	AppealInfoRequested AppealStatus = "info_requested"
	AppealRecommended   AppealStatus = "recommended" // first admin approved; awaits a second, different admin
	AppealApproved      AppealStatus = "approved"    // second admin approved; cooling-off running
	AppealDisbursed     AppealStatus = "disbursed"
	AppealRejected      AppealStatus = "rejected"
	AppealCancelled     AppealStatus = "cancelled" // the driver themselves cancelled it
)

// AppealReason is why the next of kin is asking for the funds.
type AppealReason string

const (
	AppealReasonDeceased      AppealReason = "deceased"
	AppealReasonIncapacitated AppealReason = "incapacitated"
	AppealReasonMissing       AppealReason = "missing"
	AppealReasonOther         AppealReason = "other"
)

// WithdrawalAppeal is a request, by a driver's next of kin, to release the
// driver's wallet balance. It can never pay out automatically: two distinct
// admins must approve, then a cooling-off window must pass (during which
// the driver is notified and can cancel it).
type WithdrawalAppeal struct {
	ImmutableBase

	DriverUserID string       `gorm:"type:char(36);index;not null" json:"driver_user_id"`
	NextOfKinID  string       `gorm:"type:char(36);not null" json:"next_of_kin_id"`
	NOKName      string       `gorm:"type:varchar(150);not null" json:"nok_name"`
	NOKPhone     string       `gorm:"type:varchar(20);not null" json:"nok_phone"`
	Reason       AppealReason `gorm:"type:varchar(20);not null" json:"reason"`
	Details      string       `gorm:"type:text" json:"details"`
	Status       AppealStatus `gorm:"type:varchar(20);index;not null" json:"status"`

	// Where the funds should go (resolved against the bank at submission).
	DestBankCode      string `gorm:"type:varchar(20)" json:"dest_bank_code"`
	DestBankName      string `gorm:"type:varchar(100)" json:"dest_bank_name"`
	DestAccountNumber string `gorm:"type:varchar(20)" json:"dest_account_number"`
	DestAccountName   string `gorm:"type:varchar(150)" json:"dest_account_name"`

	// TokenHash lets the next of kin upload documents and check status
	// without an account (shown once at submission).
	TokenHash      string    `gorm:"type:char(64);index" json:"-"`
	TokenExpiresAt time.Time `json:"-"`

	DriverNotifiedAt *time.Time `json:"driver_notified_at,omitempty"`
	CancelledAt      *time.Time `json:"cancelled_at,omitempty"`

	RecommendedBy    string     `gorm:"type:char(36)" json:"recommended_by,omitempty"`
	RecommendedAt    *time.Time `json:"recommended_at,omitempty"`
	ApprovedBy       string     `gorm:"type:char(36)" json:"approved_by,omitempty"`
	ApprovedAt       *time.Time `json:"approved_at,omitempty"`
	DisbursableAfter *time.Time `json:"disbursable_after,omitempty"`
	RejectedBy       string     `gorm:"type:char(36)" json:"rejected_by,omitempty"`
	RejectionReason  string     `gorm:"type:varchar(500)" json:"rejection_reason,omitempty"`
	InfoRequest      string     `gorm:"type:varchar(500)" json:"info_request,omitempty"`
	AdminNotes       string     `gorm:"type:text" json:"admin_notes,omitempty"`

	AmountKobo   int64   `json:"amount_kobo"` // set at disbursement
	WithdrawalID *string `gorm:"type:char(36)" json:"withdrawal_id,omitempty"`
}

func (WithdrawalAppeal) TableName() string { return "withdrawal_appeals" }

// AppealDocument is a supporting file (death certificate, ID, letter) for
// an appeal.
type AppealDocument struct {
	ImmutableBase

	AppealID string `gorm:"type:char(36);index;not null" json:"appeal_id"`
	Kind     string `gorm:"type:varchar(30);not null" json:"kind"`
	URL      string `gorm:"type:varchar(500);not null" json:"url"`
}

func (AppealDocument) TableName() string { return "appeal_documents" }

// AuditLog is an append-only record of sensitive actions (admin views of
// tracking data, approvals, freezes, PIN changes ...).
type AuditLog struct {
	ID          uint64    `gorm:"primaryKey;autoIncrement" json:"id"`
	ActorUserID string    `gorm:"type:char(36);index" json:"actor_user_id"`
	ActorRole   string    `gorm:"type:varchar(20)" json:"actor_role"`
	Action      string    `gorm:"type:varchar(60);index;not null" json:"action"`
	TargetType  string    `gorm:"type:varchar(30);index:idx_audit_target,priority:1" json:"target_type"`
	TargetID    string    `gorm:"type:varchar(40);index:idx_audit_target,priority:2" json:"target_id"`
	Metadata    string    `gorm:"type:text" json:"metadata,omitempty"`
	IP          string    `gorm:"type:varchar(64)" json:"ip,omitempty"`
	CreatedAt   time.Time `gorm:"precision:3;index" json:"created_at"`
}

func (AuditLog) TableName() string { return "audit_logs" }

// PlatformSetting is a small key/value store for runtime switches an admin
// can flip without a deploy (e.g. the global payout kill-switch).
type PlatformSetting struct {
	Key       string    `gorm:"type:varchar(60);primaryKey" json:"key"`
	Value     string    `gorm:"type:varchar(500);not null" json:"value"`
	UpdatedBy string    `gorm:"type:char(36)" json:"updated_by,omitempty"`
	UpdatedAt time.Time `json:"updated_at"`
}

func (PlatformSetting) TableName() string { return "platform_settings" }
