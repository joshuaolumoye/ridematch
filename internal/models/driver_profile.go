package models

import "time"

// VehicleType is the mode of transport a driver operates. This is the axis
// that makes the platform multi-modal (car, okada, keke, bus) rather than
// single-category like most incumbents.
type VehicleType string

const (
	VehicleCar   VehicleType = "car"
	VehicleOkada VehicleType = "okada" // commercial motorcycle
	VehicleKeke  VehicleType = "keke"  // tricycle
	VehicleBus   VehicleType = "bus"
)

// DriverVerificationStatus tracks manual review of a driver's submitted
// documents before they're allowed to go online and appear to passengers.
type DriverVerificationStatus string

const (
	VerificationPending  DriverVerificationStatus = "pending"
	VerificationApproved DriverVerificationStatus = "approved"
	VerificationRejected DriverVerificationStatus = "rejected"
)

// DriverProfile extends a User with the driver-specific data required to
// go online, be matched with passengers, and pay the daily platform-access
// fee. A User has at most one DriverProfile.
type DriverProfile struct {
	Base

	UserID string `gorm:"type:char(36);uniqueIndex;not null" json:"user_id"`

	VehicleType     VehicleType `gorm:"type:varchar(20);not null" json:"vehicle_type"`
	PlateNumber     string      `gorm:"type:varchar(20);uniqueIndex" json:"plate_number"`
	VehiclePhotoURL string      `gorm:"type:varchar(500)" json:"vehicle_photo_url,omitempty"`

	// IDDocumentURL points at a private object-storage key, never a public
	// URL — it is only ever resolved to a short-lived signed URL for an
	// admin performing verification. See internal/utils storage helpers.
	IDDocumentURL      string                   `gorm:"type:varchar(500)" json:"-"`
	VerificationStatus DriverVerificationStatus `gorm:"type:varchar(20);not null;default:'pending'" json:"verification_status"`
	VerificationNote   string                   `gorm:"type:varchar(500)" json:"verification_note,omitempty"`

	// SubscriptionActiveUntil gates whether the driver is allowed to go
	// online. Set/extended by the Flutterwave payment webhook.
	SubscriptionActiveUntil *time.Time `json:"subscription_active_until,omitempty"`

	IsOnline bool `gorm:"default:false" json:"is_online"`

	RatingAverage float64 `gorm:"type:decimal(3,2);default:5.00" json:"rating_average"`
	RatingCount   int64   `gorm:"default:0" json:"rating_count"`

	User *User `gorm:"foreignKey:UserID" json:"-"`
}

func (DriverProfile) TableName() string {
	return "driver_profiles"
}

// HasActiveSubscription reports whether the driver has paid for platform
// access covering the current moment.
func (d *DriverProfile) HasActiveSubscription() bool {
	return d.SubscriptionActiveUntil != nil && d.SubscriptionActiveUntil.After(time.Now())
}

// IsApproved reports whether the driver has passed manual ID verification.
func (d *DriverProfile) IsApproved() bool {
	return d.VerificationStatus == VerificationApproved
}
