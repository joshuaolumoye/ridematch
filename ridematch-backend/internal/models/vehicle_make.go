package models

// VehicleMake is one admin-entered vehicle manufacturer under a vehicle
// type — "Toyota", "Honda", "Bajaj" (okada), "Piaggio" (keke), and so
// on. Entirely admin-curated: there is no external catalog API that
// usefully covers Nigeria's market (motorcycles and tricycles especially
// aren't in any mainstream vehicle-data API), so this table *is* the
// catalog. See VehicleModel for the make -> model relationship.
type VehicleMake struct {
	Base

	VehicleType VehicleType `gorm:"type:varchar(20);not null;uniqueIndex:idx_vehicle_makes_type_name" json:"vehicle_type"`
	Name        string      `gorm:"type:varchar(80);not null;uniqueIndex:idx_vehicle_makes_type_name" json:"name"`
	// Enabled deliberately carries no `default:` gorm tag — see the
	// identical comment on VehicleTypeConfig.Enabled for why. Every write
	// path here sets it explicitly.
	Enabled bool `gorm:"not null" json:"enabled"`

	Models []VehicleModel `gorm:"foreignKey:MakeID" json:"models,omitempty"`
}

func (VehicleMake) TableName() string {
	return "vehicle_makes"
}
