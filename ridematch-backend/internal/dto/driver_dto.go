package dto

// DriverDocumentUpload is one submitted document, tagged with which
// admin-defined DocumentType it answers — see GET /meta/document-types
// for the list a vehicle type currently requires/offers.
type DriverDocumentUpload struct {
	DocumentTypeID string `json:"document_type_id" binding:"required"`
	FileURL        string `json:"file_url" binding:"required,url"`
}

// RegisterDriverRequest is the payload for POST /driver/register.
// Documents replaces the old single, hardcoded id_document_url — every
// vehicle type now has its own admin-defined list of required/optional
// documents (GET /meta/document-types?vehicle_type=...), and the
// service layer rejects registration if a required one is missing.
// Every file URL is uploaded by the client first (POST /uploads) and
// just referenced here by URL.
type RegisterDriverRequest struct {
	VehicleType     string                 `json:"vehicle_type" binding:"required,oneof=car okada keke bus" example:"okada"`
	PlateNumber     string                 `json:"plate_number" binding:"required,max=20" example:"KJA-123-XY"`
	VehiclePhotoURL string                 `json:"vehicle_photo_url" binding:"omitempty,url"`
	Documents       []DriverDocumentUpload `json:"documents" binding:"omitempty,dive"`

	// Make/model, picked from GET /meta/vehicle-catalog?vehicle_type=...
	// — optional so registration still works for a vehicle type admin
	// hasn't populated the catalog for yet (the app falls back to free-
	// text entry in that case). When ModelID is given it must belong to
	// MakeID, and both must be enabled, currently-cataloged entries for
	// this VehicleType.
	VehicleMakeID  *string `json:"vehicle_make_id" binding:"omitempty"`
	VehicleModelID *string `json:"vehicle_model_id" binding:"omitempty"`
	// Free-text fallback, used only when the catalog has no entry for
	// this driver's exact make/model yet (or no catalog at all for this
	// vehicle type) — never sent alongside VehicleMakeID/VehicleModelID.
	VehicleMakeFreeText  string `json:"vehicle_make_free_text" binding:"omitempty,max=80"`
	VehicleModelFreeText string `json:"vehicle_model_free_text" binding:"omitempty,max=80"`
	// Year of manufacture — a plain bounded integer, not a catalog
	// lookup (see VehicleModel's doc comment for why).
	VehicleYear *int `json:"vehicle_year" binding:"omitempty,min=1980"`
}

// DriverProfileResponse is the public-safe representation of a driver's
// profile. IDDocumentURL is deliberately omitted — it is only ever
// resolved for an admin performing verification, never returned to the
// driver themselves or anyone else over this API.
type DriverProfileResponse struct {
	ID                      string  `json:"id"`
	UserID                  string  `json:"user_id"`
	VehicleType             string  `json:"vehicle_type"`
	PlateNumber             string  `json:"plate_number"`
	VehiclePhotoURL         string  `json:"vehicle_photo_url,omitempty"`
	VehicleMakeName         string  `json:"vehicle_make_name,omitempty"`
	VehicleModelName        string  `json:"vehicle_model_name,omitempty"`
	VehicleYear             *int    `json:"vehicle_year,omitempty"`
	VehicleCategory         string  `json:"vehicle_category,omitempty"`
	VerificationStatus      string  `json:"verification_status"`
	VerificationNote        string  `json:"verification_note,omitempty"`
	IsOnline                bool    `json:"is_online"`
	HasActiveSubscription   bool    `json:"has_active_subscription"`
	SubscriptionActiveUntil string  `json:"subscription_active_until,omitempty"`
	RatingAverage           float64 `json:"rating_average"`
}

// GoOnlineRequest is the payload for POST /driver/online — the driver's
// current position, required so they're immediately visible to nearby
// passengers rather than only after their first periodic ping.
type GoOnlineRequest struct {
	Latitude  float64 `json:"latitude" binding:"required,min=-90,max=90"`
	Longitude float64 `json:"longitude" binding:"required,min=-180,max=180"`
}

// LocationPingRequest is the payload for POST /driver/location, sent
// periodically (e.g. every 5-10s) by the app while a driver is online.
type LocationPingRequest struct {
	Latitude  float64 `json:"latitude" binding:"required,min=-90,max=90"`
	Longitude float64 `json:"longitude" binding:"required,min=-180,max=180"`
}

// NearbyDriverResponse is one row in the passenger-facing nearby-drivers
// list. Coordinates are rounded (see NEARBY_COORDINATE_PRECISION) rather
// than exact, and the driver's plate number is withheld until a match is
// confirmed — a passenger browsing the map sees enough to pick a driver,
// not enough to track or identify their vehicle from a distance.
type NearbyDriverResponse struct {
	DriverID    string `json:"driver_id"`
	Name        string `json:"name"`
	PhotoURL    string `json:"photo_url,omitempty"`
	VehicleType string `json:"vehicle_type"`
	// VehicleCategory is only ever set for VehicleType "car" — the
	// Economy/Comfort/SUV/Premium bucket the passenger's booking-screen
	// category chips filter by. Filtering happens client-side against
	// this already-fetched list (nearby-driver counts are small — tens,
	// not thousands — so there's no need for a server-side query param).
	VehicleCategory string  `json:"vehicle_category,omitempty"`
	RatingAverage   float64 `json:"rating_average"`
	DistanceKM      float64 `json:"distance_km"`
	Latitude        float64 `json:"latitude"`
	Longitude       float64 `json:"longitude"`
}

// AdminVerifyDriverRequest is the payload for
// PATCH /admin/drivers/:id/verify.
type AdminVerifyDriverRequest struct {
	Approve bool   `json:"approve"`
	Note    string `json:"note" binding:"omitempty,max=500"`
}

// AdminActivateSubscriptionRequest is the payload for
// PATCH /admin/drivers/:id/subscription — a manual stand-in for the
// Flutterwave webhook that will call the same service method once
// payment integration lands.
type AdminActivateSubscriptionRequest struct {
	Days int `json:"days" binding:"required,min=1,max=365" example:"1"`
}
