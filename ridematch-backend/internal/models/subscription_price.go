package models

import "time"

// SubscriptionPrice is the admin-configurable daily platform-access fee
// for one vehicle type. There is exactly one row per VehicleType — okada,
// keke, car, and bus riders pay different daily rates, set by staff from
// the admin dashboard rather than hard-coded in config.
type SubscriptionPrice struct {
	// VehicleType is the primary key: one price per vehicle type, not a
	// UUID-keyed row, since there are only ever exactly four of these
	// and "the price for okada" is a more natural lookup than an ID.
	VehicleType     VehicleType `gorm:"type:varchar(20);primaryKey" json:"vehicle_type"`
	PriceKoboPerDay int64       `gorm:"not null" json:"price_kobo_per_day"`
	UpdatedAt       time.Time   `json:"updated_at"`
}

func (SubscriptionPrice) TableName() string {
	return "subscription_prices"
}
