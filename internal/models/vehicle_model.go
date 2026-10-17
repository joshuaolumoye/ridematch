package models

// VehicleCategory is the passenger-facing ride tier a car model belongs
// to — the icon a passenger taps on the booking screen (Economy,
// Comfort, SUV, Premium). Only meaningful for VehicleCar; okada, keke,
// and bus models leave it empty since those vehicle types don't have
// tiers in this market.
type VehicleCategory string

const (
	CategoryEconomy VehicleCategory = "economy"
	CategoryComfort VehicleCategory = "comfort"
	CategorySUV     VehicleCategory = "suv"
	CategoryPremium VehicleCategory = "premium"
)

// IsValidVehicleCategory reports whether v is one of the known category
// values, or empty (no category set — valid for every vehicle type, and
// the only valid state for okada/keke/bus).
func IsValidVehicleCategory(v string) bool {
	switch VehicleCategory(v) {
	case "", CategoryEconomy, CategoryComfort, CategorySUV, CategoryPremium:
		return true
	default:
		return false
	}
}

// VehicleModel is one admin-entered model/trim under a VehicleMake —
// "Camry LE", "Camry XLE", "Yaris", "Hiace" and so on. Year of
// manufacture is deliberately NOT stored here: it's just a bounded
// integer (say 1990 to next year) with no catalog of its own, so the
// registration screen generates that dropdown locally instead of
// needing admin data entry for it.
type VehicleModel struct {
	Base

	MakeID string `gorm:"type:char(36);not null;uniqueIndex:idx_vehicle_models_make_name" json:"make_id"`
	Name   string `gorm:"type:varchar(80);not null;uniqueIndex:idx_vehicle_models_make_name" json:"name"`

	// Category only applies to car models (see VehicleCategory) — left
	// empty for every other vehicle type.
	Category VehicleCategory `gorm:"type:varchar(20)" json:"category,omitempty"`

	// Enabled deliberately carries no `default:` gorm tag — see the
	// identical comment on VehicleTypeConfig.Enabled for why.
	Enabled bool `gorm:"not null" json:"enabled"`

	Make *VehicleMake `gorm:"foreignKey:MakeID" json:"-"`
}

func (VehicleModel) TableName() string {
	return "vehicle_models"
}
