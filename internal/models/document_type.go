package models

// DocumentType is one admin-defined document a driver of a given
// vehicle type must (or may optionally) submit during registration —
// e.g. "Driver's License" (required) or "Local Government ID"
// (optional) for car drivers. Admins fully control this list per
// vehicle type from the dashboard; the mobile registration screen
// renders exactly these, in SortOrder, with nothing hardcoded.
type DocumentType struct {
	Base

	VehicleType VehicleType `gorm:"type:varchar(20);not null;index" json:"vehicle_type"`
	Name        string      `gorm:"type:varchar(120);not null" json:"name"`
	// Required and Enabled deliberately carry no `default:` gorm tag.
	// GORM's Create() defers a field to its DB-level default whenever the
	// Go value is that type's zero value (false, for a bool) — which
	// silently turned an explicit "required: false" or "enabled: false"
	// on creation into `true` (the old default:true tag's value) instead
	// of persisting the false the caller actually sent. Every write path
	// here (CreateDocumentType, UpdateDocumentType) always sets both
	// fields explicitly, so no DB default is needed, and leaving the tag
	// off removes the ambiguity entirely rather than relying on callers
	// to route zero-value writes through Save/Updates instead of Create.
	Required  bool `gorm:"not null" json:"required"`
	Enabled   bool `gorm:"not null" json:"enabled"`
	SortOrder int  `gorm:"not null;default:0" json:"sort_order"`
}

func (DocumentType) TableName() string {
	return "document_types"
}
