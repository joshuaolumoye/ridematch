package models

import "time"

// OTPPurpose scopes a code to the flow it was issued for, so a code
// requested for login can't be replayed to authorize something else.
type OTPPurpose string

const (
	OTPPurposeLogin OTPPurpose = "login" // covers both registration and login: one flow, phone + OTP
)

// OTPRequest is a one-time-password challenge sent to a phone number.
// Only a bcrypt hash of the code is stored, never the plaintext, so a
// database leak doesn't hand out valid codes.
type OTPRequest struct {
	Base

	Phone       string     `gorm:"type:varchar(20);index;not null" json:"phone"`
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
