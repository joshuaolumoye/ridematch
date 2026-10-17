package dto

// AdminVehicleTypeItem is one vehicle type's enabled/disabled switch —
// GET/PUT /admin/settings/vehicle-types.
type AdminVehicleTypeItem struct {
	VehicleType string `json:"vehicle_type"`
	Enabled     bool   `json:"enabled"`
	UpdatedAt   string `json:"updated_at"`
}

// AdminUpdateVehicleTypeRequest is the payload for
// PUT /admin/settings/vehicle-types/:vehicle_type.
type AdminUpdateVehicleTypeRequest struct {
	Enabled bool `json:"enabled"`
}

// AdminSubscriptionPlanItem is one (vehicle type, billing cycle) plan —
// GET/PUT /admin/settings/subscription-plans.
type AdminSubscriptionPlanItem struct {
	VehicleType  string `json:"vehicle_type"`
	BillingCycle string `json:"billing_cycle"`
	Enabled      bool   `json:"enabled"`
	PriceKobo    int64  `json:"price_kobo"`
	UpdatedAt    string `json:"updated_at"`
}

// AdminUpdateSubscriptionPlanRequest is the payload for
// PUT /admin/settings/subscription-plans/:vehicle_type/:billing_cycle.
type AdminUpdateSubscriptionPlanRequest struct {
	Enabled   bool  `json:"enabled"`
	PriceKobo int64 `json:"price_kobo" binding:"min=0" example:"350000"`
}

// AdminDocumentTypeItem is one admin-defined document requirement for a
// vehicle type — GET/POST/PUT/DELETE /admin/settings/document-types.
type AdminDocumentTypeItem struct {
	ID          string `json:"id"`
	VehicleType string `json:"vehicle_type"`
	Name        string `json:"name"`
	Required    bool   `json:"required"`
	Enabled     bool   `json:"enabled"`
	SortOrder   int    `json:"sort_order"`
}

// AdminCreateDocumentTypeRequest is the payload for
// POST /admin/settings/document-types.
type AdminCreateDocumentTypeRequest struct {
	VehicleType string `json:"vehicle_type" binding:"required,oneof=car okada keke bus" example:"car"`
	Name        string `json:"name" binding:"required,max=120" example:"Driver's License"`
	Required    bool   `json:"required"`
	SortOrder   int    `json:"sort_order"`
}

// AdminUpdateDocumentTypeRequest is the payload for
// PUT /admin/settings/document-types/:id.
type AdminUpdateDocumentTypeRequest struct {
	Name      string `json:"name" binding:"required,max=120"`
	Required  bool   `json:"required"`
	Enabled   bool   `json:"enabled"`
	SortOrder int    `json:"sort_order"`
}

// VehicleTypeItem is one currently-enabled vehicle type — the
// driver-facing/public shape, with no admin-only fields.
// GET /meta/vehicle-types.
type VehicleTypeItem struct {
	VehicleType string `json:"vehicle_type"`
}

// DocumentTypeItem is one currently-enabled document requirement for a
// vehicle type — the driver-facing shape the registration screen
// renders. GET /meta/document-types.
type DocumentTypeItem struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Required  bool   `json:"required"`
	SortOrder int    `json:"sort_order"`
}
