package models

import "time"

// TripStatus is the state of a trip request through its lifecycle:
//
//	requested -> matched -> picked_up -> completed
//	requested -> expired            (no driver matched in time)
//	requested|matched -> cancelled  (either party, only before pickup)
type TripStatus string

const (
	TripRequested TripStatus = "requested"
	TripMatched   TripStatus = "matched"
	TripPickedUp  TripStatus = "picked_up"
	TripCompleted TripStatus = "completed"
	TripCancelled TripStatus = "cancelled"
	TripExpired   TripStatus = "expired"
)

// CancelledBy records which side ended an unfinished trip, for dispute
// review and driver/passenger reliability metrics later.
type CancelledBy string

const (
	CancelledByPassenger CancelledBy = "passenger"
	CancelledByDriver    CancelledBy = "driver"
	CancelledBySystem    CancelledBy = "system" // e.g. expiry sweep
)

// Trip is a passenger's ride/logistics request: where they are, where
// they're going, and its progress through negotiation, matching, pickup
// confirmation, and completion. Trip fares settle offline between
// passenger and driver — Trip.AgreedPriceKobo is a record of what was
// agreed, not a charge the platform processes.
type Trip struct {
	Base

	PassengerID string      `gorm:"type:char(36);index;not null" json:"passenger_id"`
	VehicleType VehicleType `gorm:"type:varchar(20);index:idx_status_vehicle,priority:2;not null" json:"vehicle_type"`
	Status      TripStatus  `gorm:"type:varchar(20);index:idx_status_vehicle,priority:1;not null;default:'requested'" json:"status"`

	PickupLat          float64 `gorm:"not null" json:"pickup_lat"`
	PickupLng          float64 `gorm:"not null" json:"pickup_lng"`
	PickupAddress      string  `gorm:"type:varchar(255)" json:"pickup_address,omitempty"`
	DestinationLat     float64 `gorm:"not null" json:"destination_lat"`
	DestinationLng     float64 `gorm:"not null" json:"destination_lng"`
	DestinationAddress string  `gorm:"type:varchar(255)" json:"destination_address,omitempty"`

	OfferedPriceKobo int64  `gorm:"not null" json:"offered_price_kobo"`
	AgreedPriceKobo  *int64 `json:"agreed_price_kobo,omitempty"`

	// DriverID is set only once a driver's offer is accepted.
	DriverID *string `gorm:"type:char(36);index" json:"driver_id,omitempty"`

	// PickupPINHash is a bcrypt hash of the 4-digit code the passenger
	// shows the driver in person to confirm pickup actually happened —
	// never returned to the driver, only compared server-side.
	PickupPINHash     string     `gorm:"type:varchar(100)" json:"-"`
	PickupConfirmedAt *time.Time `json:"pickup_confirmed_at,omitempty"`
	CompletedAt       *time.Time `json:"completed_at,omitempty"`

	CancelledAt  *time.Time   `json:"cancelled_at,omitempty"`
	CancelledBy  *CancelledBy `gorm:"type:varchar(20)" json:"cancelled_by,omitempty"`
	CancelReason string       `gorm:"type:varchar(255)" json:"cancel_reason,omitempty"`

	Passenger *User          `gorm:"foreignKey:PassengerID" json:"-"`
	Driver    *DriverProfile `gorm:"foreignKey:DriverID" json:"-"`
}

func (Trip) TableName() string {
	return "trips"
}

// IsOpen reports whether the trip can still receive/accept offers.
func (t *Trip) IsOpen() bool {
	return t.Status == TripRequested
}
