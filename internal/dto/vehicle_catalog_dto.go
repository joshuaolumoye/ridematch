package dto

// --- Admin (full management view: enabled and disabled alike) ---

// AdminVehicleModelItem is one model/trim under a make, as the admin
// catalog editor renders it.
type AdminVehicleModelItem struct {
	ID       string `json:"id"`
	MakeID   string `json:"make_id"`
	Name     string `json:"name"`
	Category string `json:"category,omitempty"`
	Enabled  bool   `json:"enabled"`
}

// AdminVehicleMakeItem is one make with its models nested — the shape
// the admin catalog page's expandable make/model tree renders directly.
type AdminVehicleMakeItem struct {
	ID          string                  `json:"id"`
	VehicleType string                  `json:"vehicle_type"`
	Name        string                  `json:"name"`
	Enabled     bool                    `json:"enabled"`
	Models      []AdminVehicleModelItem `json:"models"`
}

// AdminCreateMakeRequest is the payload for
// POST /admin/vehicle-catalog/makes.
type AdminCreateMakeRequest struct {
	VehicleType string `json:"vehicle_type" binding:"required,oneof=car okada keke bus"`
	Name        string `json:"name" binding:"required,max=80"`
}

// AdminUpdateMakeRequest is the payload for
// PUT /admin/vehicle-catalog/makes/:id.
type AdminUpdateMakeRequest struct {
	Name    string `json:"name" binding:"required,max=80"`
	Enabled bool   `json:"enabled"`
}

// AdminCreateModelRequest is the payload for
// POST /admin/vehicle-catalog/models.
type AdminCreateModelRequest struct {
	MakeID   string `json:"make_id" binding:"required"`
	Name     string `json:"name" binding:"required,max=80"`
	Category string `json:"category" binding:"omitempty,oneof=economy comfort suv premium"`
}

// AdminUpdateModelRequest is the payload for
// PUT /admin/vehicle-catalog/models/:id.
type AdminUpdateModelRequest struct {
	Name     string `json:"name" binding:"required,max=80"`
	Category string `json:"category" binding:"omitempty,oneof=economy comfort suv premium"`
	Enabled  bool   `json:"enabled"`
}

// AdminBulkCatalogRow is one "Make, Model[, Category]" row from the
// admin dashboard's paste-many-rows-at-once importer.
type AdminBulkCatalogRow struct {
	Make     string `json:"make" binding:"required,max=80"`
	Model    string `json:"model" binding:"required,max=80"`
	Category string `json:"category" binding:"omitempty,oneof=economy comfort suv premium"`
}

// AdminBulkCatalogRequest is the payload for
// POST /admin/vehicle-catalog/bulk — pasted/imported rows, all for one
// vehicle type at a time (the importer UI is scoped to one vehicle type
// tab, so every row shares it).
type AdminBulkCatalogRequest struct {
	VehicleType string                `json:"vehicle_type" binding:"required,oneof=car okada keke bus"`
	Rows        []AdminBulkCatalogRow `json:"rows" binding:"required,min=1,max=500,dive"`
}

// AdminBulkCatalogResult summarizes what a bulk import actually did —
// shown back to the admin so a re-paste of an already-imported sheet
// visibly no-ops instead of looking like it silently failed.
type AdminBulkCatalogResult struct {
	MakesCreated  int `json:"makes_created"`
	ModelsCreated int `json:"models_created"`
	// Skipped counts rows whose make+model combination already existed
	// (case-insensitive) — not an error, just a no-op for that row.
	Skipped int `json:"skipped"`
}

// --- Public / driver + passenger facing (enabled only) ---

// VehicleCatalogModelItem is one sellable model for the registration
// and booking screens to render.
type VehicleCatalogModelItem struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Category string `json:"category,omitempty"`
}

// VehicleCatalogMakeItem is one make with its enabled models nested —
// GET /meta/vehicle-catalog's shape.
type VehicleCatalogMakeItem struct {
	ID     string                    `json:"id"`
	Name   string                    `json:"name"`
	Models []VehicleCatalogModelItem `json:"models"`
}
