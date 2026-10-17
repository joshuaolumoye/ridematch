package models

import "time"

// VehicleTypeConfig is the admin-configurable on/off switch for one
// vehicle type. There is exactly one row per VehicleType (car, okada,
// keke, bus) — when Enabled is false, that vehicle type is hidden from
// registration (new drivers can't pick it) and from the passenger
// booking screen's mode picker, without needing a code deploy. It does
// not affect drivers already registered under that type.
type VehicleTypeConfig struct {
	VehicleType VehicleType `gorm:"type:varchar(20);primaryKey" json:"vehicle_type"`
	// Enabled deliberately carries no `default:` gorm tag. GORM's
	// Create() (used here via Upsert's ON DUPLICATE KEY UPDATE) defers a
	// field to its DB-level default whenever the Go value is that type's
	// zero value — for a bool, that's `false`. With a `default:true` tag,
	// every attempt to disable a vehicle type (Enabled: false, the zero
	// value) silently got overwritten with `true` on both insert and
	// conflict-update, because GORM substituted the tag's default into
	// the generated SQL instead of the real false it was given — the
	// admin dashboard would show "turned off" (the API call succeeded)
	// but the vehicle type stayed enabled. SetVehicleTypeEnabled always
	// sets this field explicitly, so no DB default is needed, and
	// dropping the tag removes the ambiguity at the source.
	Enabled   bool      `gorm:"not null" json:"enabled"`
	UpdatedAt time.Time `json:"updated_at"`
}

func (VehicleTypeConfig) TableName() string {
	return "vehicle_type_configs"
}
