package models

import "time"

// RefreshToken persists issued refresh tokens (hashed) so they can be
// revoked individually (logout, password/device compromise) rather than
// relying purely on JWT expiry. Only a SHA-256 hash of the token is stored.
type RefreshToken struct {
	Base

	UserID    string     `gorm:"type:char(36);index;not null" json:"user_id"`
	TokenHash string     `gorm:"type:varchar(64);uniqueIndex;not null" json:"-"`
	ExpiresAt time.Time  `gorm:"not null" json:"expires_at"`
	RevokedAt *time.Time `json:"revoked_at,omitempty"`

	UserAgent string `gorm:"type:varchar(255)" json:"user_agent,omitempty"`
	IPAddress string `gorm:"type:varchar(45)" json:"ip_address,omitempty"`
}

func (RefreshToken) TableName() string {
	return "refresh_tokens"
}

// IsValid reports whether the token can still be used to mint a new access
// token: not expired and not explicitly revoked.
func (r *RefreshToken) IsValid() bool {
	return r.RevokedAt == nil && time.Now().Before(r.ExpiresAt)
}
