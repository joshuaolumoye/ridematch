package models

import "time"

// UserRole distinguishes how an account primarily uses the platform.
// A single phone number maps to exactly one User; a person who wants to
// drive registers/activates a DriverProfile against that same account
// rather than creating a second one.
type UserRole string

const (
	RoleUser   UserRole = "user"   // requests rides / sends logistics requests
	RoleDriver UserRole = "driver" // has an associated DriverProfile
	RoleAdmin  UserRole = "admin"  // internal staff (verification, support)
)

// UserStatus controls whether an account is allowed to use the platform.
type UserStatus string

const (
	StatusActive    UserStatus = "active"
	StatusSuspended UserStatus = "suspended" // temporary, reversible restriction
	StatusBanned    UserStatus = "banned"    // permanent restriction
)

// User is the single identity record for anyone on the platform, whether
// they only ever request rides or also drive. Authentication is OTP
// based — by phone or by email, a person's choice at sign-up — there is
// no password field by design.
//
// Exactly one of Phone/Email is required (enforced in AuthService, not
// the database): an account created via phone has Email empty, one
// created via email has Phone empty. Both columns are plain (non-unique)
// indexes rather than unique constraints — a unique constraint on a
// nullable varchar can't tell "no value" from "the empty string" the way
// SQL NULL can, and every place in this codebase already treats Phone as
// a plain Go string rather than a pointer, so keeping that and enforcing
// uniqueness in AuthService.findOrCreateUser (look up by the identifier,
// only create if not found) was the lower-risk choice over threading
// *string through every caller for a database-level guarantee this app's
// scale doesn't need yet.
type User struct {
	Base

	Phone           string     `gorm:"type:varchar(20);index" json:"phone,omitempty"`
	Email           string     `gorm:"type:varchar(255);index" json:"email,omitempty"`
	Name            string     `gorm:"type:varchar(120)" json:"name"`
	PhotoURL        string     `gorm:"type:varchar(500)" json:"photo_url,omitempty"`
	Role            UserRole   `gorm:"type:varchar(20);not null;default:'user'" json:"role"`
	Status          UserStatus `gorm:"type:varchar(20);not null;default:'active'" json:"status"`
	PhoneVerifiedAt *time.Time `json:"phone_verified_at,omitempty"`
	EmailVerifiedAt *time.Time `json:"email_verified_at,omitempty"`

	// PushToken is the device's Expo push token, registered by the app
	// after login (see PATCH /users/me/push-token). One token per
	// account — a login on a new device overwrites the old one, which is
	// fine since a push failing to reach a now-inactive device is
	// harmless (REST + the in-app notifications list are the source of
	// truth either way).
	PushToken string `gorm:"type:varchar(255)" json:"-"`

	RatingAverage float64 `gorm:"type:decimal(3,2);default:5.00" json:"rating_average"`
	RatingCount   int64   `gorm:"default:0" json:"rating_count"`

	DriverProfile *DriverProfile `gorm:"foreignKey:UserID" json:"driver_profile,omitempty"`
}

// TableName pins the table name explicitly so it never silently changes if
// GORM's pluralization rules change between versions.
func (User) TableName() string {
	return "users"
}
