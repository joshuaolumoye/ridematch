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
// they only ever request rides or also drive. Authentication is phone +
// OTP based; there is no password field by design.
type User struct {
	Base

	Phone           string     `gorm:"type:varchar(20);uniqueIndex;not null" json:"phone"`
	Name            string     `gorm:"type:varchar(120)" json:"name"`
	PhotoURL        string     `gorm:"type:varchar(500)" json:"photo_url,omitempty"`
	Role            UserRole   `gorm:"type:varchar(20);not null;default:'user'" json:"role"`
	Status          UserStatus `gorm:"type:varchar(20);not null;default:'active'" json:"status"`
	PhoneVerifiedAt *time.Time `json:"phone_verified_at,omitempty"`

	RatingAverage float64 `gorm:"type:decimal(3,2);default:5.00" json:"rating_average"`
	RatingCount   int64   `gorm:"default:0" json:"rating_count"`

	DriverProfile *DriverProfile `gorm:"foreignKey:UserID" json:"driver_profile,omitempty"`
}

// TableName pins the table name explicitly so it never silently changes if
// GORM's pluralization rules change between versions.
func (User) TableName() string {
	return "users"
}
