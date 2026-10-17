package models

import "time"

// OTPPurpose scopes a code to the flow it was issued for, so a code
// requested for login can't be replayed to authorize something else.
type OTPPurpose string

const (
	OTPPurposeLogin OTPPurpose = "login" // covers both registration and login: one flow, identifier + OTP
)

// OTPChannel says how a code was delivered. Deliberately a separate type
// from utils.Channel (rather than importing it) — internal/utils already
// imports internal/models (for JWT claims), so importing utils back here
// would be a cycle; the two types share the same two string values and
// AuthService converts between them at the one call site that needs to.
type OTPChannel string

const (
	OTPChannelPhone OTPChannel = "phone"
	OTPChannelEmail OTPChannel = "email"
)

// OTPRequest is a one-time-password challenge sent to a phone number or
// email address. Only a bcrypt hash of the code is stored, never the
// plaintext, so a database leak doesn't hand out valid codes.
type OTPRequest struct {
	Base

	// Identifier is the normalized phone number (+234...) or email
	// address (lowercased) the code was sent to; Channel says which.
	Identifier  string     `gorm:"column:identifier;type:varchar(255);index;not null" json:"identifier"`
	Channel     OTPChannel `gorm:"type:varchar(10);not null;default:'phone'" json:"channel"`
	CodeHash    string     `gorm:"type:varchar(100);not null" json:"-"`
	Purpose     OTPPurpose `gorm:"type:varchar(20);not null" json:"purpose"`
	ExpiresAt   time.Time  `gorm:"not null" json:"expires_at"`
	Attempts    int        `gorm:"default:0" json:"-"`
	MaxAttempts int        `gorm:"default:5" json:"-"`
	ConsumedAt  *time.Time `json:"-"`
}

func (OTPRequest) TableName() string {
	return "otp_requests"
}

// IsExpired reports whether the code can no longer be verified.
func (o *OTPRequest) IsExpired() bool {
	return time.Now().After(o.ExpiresAt)
}

// IsConsumed reports whether the code has already been successfully used.
func (o *OTPRequest) IsConsumed() bool {
	return o.ConsumedAt != nil
}

// AttemptsExceeded reports whether too many wrong guesses have been made
// against this code, so it should be rejected even if not expired.
func (o *OTPRequest) AttemptsExceeded() bool {
	return o.Attempts >= o.MaxAttempts
}
