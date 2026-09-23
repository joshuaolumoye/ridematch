package dto

// CreateTripRequest is the payload for POST /trips.
type CreateTripRequest struct {
	VehicleType        string  `json:"vehicle_type" binding:"required,oneof=car okada keke bus" example:"okada"`
	PickupLat          float64 `json:"pickup_lat" binding:"required,min=-90,max=90"`
	PickupLng          float64 `json:"pickup_lng" binding:"required,min=-180,max=180"`
	PickupAddress      string  `json:"pickup_address" binding:"omitempty,max=255"`
	DestinationLat     float64 `json:"destination_lat" binding:"required,min=-90,max=90"`
	DestinationLng     float64 `json:"destination_lng" binding:"required,min=-180,max=180"`
	DestinationAddress string  `json:"destination_address" binding:"omitempty,max=255"`
	// OfferedPriceKobo is the passenger's asking price, in kobo (NGN * 100)
	// to avoid floating-point currency bugs.
	OfferedPriceKobo int64 `json:"offered_price_kobo" binding:"required,min=1"`
}

// TripResponse is the trip record returned to its passenger or matched
// driver. It never includes the pickup PIN — that's returned exactly
// once, in AcceptOfferResponse, at the moment a match is confirmed.
type TripResponse struct {
	ID                 string  `json:"id"`
	PassengerID        string  `json:"passenger_id"`
	DriverID           string  `json:"driver_id,omitempty"`
	VehicleType        string  `json:"vehicle_type"`
	Status             string  `json:"status"`
	PickupLat          float64 `json:"pickup_lat"`
	PickupLng          float64 `json:"pickup_lng"`
	PickupAddress      string  `json:"pickup_address,omitempty"`
	DestinationLat     float64 `json:"destination_lat"`
	DestinationLng     float64 `json:"destination_lng"`
	DestinationAddress string  `json:"destination_address,omitempty"`
	OfferedPriceKobo   int64   `json:"offered_price_kobo"`
	AgreedPriceKobo    int64   `json:"agreed_price_kobo,omitempty"`
	PickupConfirmedAt  string  `json:"pickup_confirmed_at,omitempty"`
	CompletedAt        string  `json:"completed_at,omitempty"`
	CancelledAt        string  `json:"cancelled_at,omitempty"`
	CancelledBy        string  `json:"cancelled_by,omitempty"`
	CancelReason       string  `json:"cancel_reason,omitempty"`
	CreatedAt          string  `json:"created_at"`

	// DriverInfo/PassengerInfo are populated once a driver is matched —
	// enough for each party to identify the other in person, nothing more.
	DriverInfo    *TripPartyInfo `json:"driver_info,omitempty"`
	PassengerInfo *TripPartyInfo `json:"passenger_info,omitempty"`

	// PassengerRated/DriverRated report whether that side has already
	// submitted its POST /trips/:id/rate call — only meaningful once the
	// trip is completed, and only computed for a completed trip (left
	// false otherwise, since neither side can rate before then).
	PassengerRated bool `json:"passenger_rated"`
	DriverRated    bool `json:"driver_rated"`
}

// TripPartyInfo is the minimal identifying info shared between a matched
// passenger and driver.
type TripPartyInfo struct {
	Name        string  `json:"name"`
	PhotoURL    string  `json:"photo_url,omitempty"`
	Phone       string  `json:"phone,omitempty"`
	VehicleType string  `json:"vehicle_type,omitempty"`
	PlateNumber string  `json:"plate_number,omitempty"`
	Rating      float64 `json:"rating,omitempty"`
}

// MakeOfferRequest is the payload for POST /trips/:id/offers.
type MakeOfferRequest struct {
	PriceKobo int64 `json:"price_kobo" binding:"required,min=1"`
}

// OfferResponse is a driver's offer against a trip.
type OfferResponse struct {
	ID        string         `json:"id"`
	TripID    string         `json:"trip_id"`
	PriceKobo int64          `json:"price_kobo"`
	Status    string         `json:"status"`
	Driver    *TripPartyInfo `json:"driver,omitempty"`
	CreatedAt string         `json:"created_at"`
}

// AcceptOfferResponse wraps the now-matched trip plus the one-time
// plaintext pickup PIN — the only place it's ever returned.
type AcceptOfferResponse struct {
	Trip      TripResponse `json:"trip"`
	PickupPIN string       `json:"pickup_pin"`
}

// NearbyOpenTripResponse is one row in the driver-facing nearby-open-trips
// list. Coordinates are rounded, and no passenger identity is included —
// symmetric to NearbyDriverResponse's treatment of driver location before
// a match exists.
type NearbyOpenTripResponse struct {
	TripID           string  `json:"trip_id"`
	VehicleType      string  `json:"vehicle_type"`
	PickupLat        float64 `json:"pickup_lat"`
	PickupLng        float64 `json:"pickup_lng"`
	DistanceKM       float64 `json:"distance_km"`
	OfferedPriceKobo int64   `json:"offered_price_kobo"`
}

// TripHistoryResponse is a page of a passenger's trips, newest first,
// regardless of status — completed, cancelled, expired, or still active.
type TripHistoryResponse struct {
	Trips    []TripResponse `json:"trips"`
	Page     int            `json:"page"`
	PageSize int            `json:"page_size"`
	Total    int64          `json:"total"`
}

// DriverEarningsResponse is a driver's earnings summary — real,
// SQL-aggregated totals over their own completed trips, plus a recent-
// trips page for context. Fares settle offline (see Trip.AgreedPriceKobo's
// doc comment), so this is a record of what was agreed, not a platform
// ledger.
type DriverEarningsResponse struct {
	TotalEarnedKobo int64          `json:"total_earned_kobo"`
	TotalTripsDone  int64          `json:"total_trips_done"`
	TodayEarnedKobo int64          `json:"today_earned_kobo"`
	TodayTripsDone  int64          `json:"today_trips_done"`
	RecentTrips     []TripResponse `json:"recent_trips"`
}

// ConfirmPickupRequest is the payload for POST /trips/:id/confirm-pickup.
type ConfirmPickupRequest struct {
	PIN string `json:"pin" binding:"required,len=4,numeric"`
}

// CancelTripRequest is the payload for POST /trips/:id/cancel.
type CancelTripRequest struct {
	Reason string `json:"reason" binding:"omitempty,max=255"`
}

// SubmitRatingRequest is the payload for POST /trips/:id/rate.
type SubmitRatingRequest struct {
	Rating  int    `json:"rating" binding:"required,min=1,max=5" example:"5"`
	Comment string `json:"comment" binding:"omitempty,max=500"`
}
