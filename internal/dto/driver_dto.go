package dto

// RegisterDriverRequest is the payload for POST /driver/register. The
// document/photo fields are plain URLs for now (uploaded by the client to
// object storage directly, or via a future /uploads endpoint) rather than
// multipart file uploads, to keep this milestone focused on the matching
// mechanics; see README for the storage follow-up.
type RegisterDriverRequest struct {
	VehicleType     string `json:"vehicle_type" binding:"required,oneof=car okada keke bus" example:"okada"`
	PlateNumber     string `json:"plate_number" binding:"required,max=20" example:"KJA-123-XY"`
	VehiclePhotoURL string `json:"vehicle_photo_url" binding:"omitempty,url"`
	IDDocumentURL   string `json:"id_document_url" binding:"required,url"`
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
	DriverID      string  `json:"driver_id"`
	Name          string  `json:"name"`
	PhotoURL      string  `json:"photo_url,omitempty"`
	VehicleType   string  `json:"vehicle_type"`
	RatingAverage float64 `json:"rating_average"`
	DistanceKM    float64 `json:"distance_km"`
	Latitude      float64 `json:"latitude"`
	Longitude     float64 `json:"longitude"`
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
