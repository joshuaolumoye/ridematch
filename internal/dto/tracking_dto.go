package dto

import "time"

// TrackingPointInput is one GPS sample from the driver's phone.
type TrackingPointInput struct {
	// N is a per-shift counter the app increments for every sample it
	// records (persisted on the device). The server ignores any N it has
	// already stored, so a retried upload can never duplicate points.
	N          int64   `json:"n" binding:"required,min=1"`
	Lat        float64 `json:"lat" binding:"required,min=-90,max=90"`
	Lng        float64 `json:"lng" binding:"required,min=-180,max=180"`
	AccuracyM  float32 `json:"accuracy_m"`
	SpeedMps   float32 `json:"speed_mps"`
	Heading    float32 `json:"heading"`
	IsMock     bool    `json:"is_mock"`
	RecordedAt string  `json:"recorded_at" binding:"required"` // RFC3339, device clock
}

// TrackingBatchRequest uploads buffered GPS samples.
type TrackingBatchRequest struct {
	ShiftID string               `json:"shift_id" binding:"required"`
	Points  []TrackingPointInput `json:"points" binding:"required,min=1,max=200,dive"`
}

// TrackingBatchResponse acknowledges a batch.
type TrackingBatchResponse struct {
	Accepted      int   `json:"accepted"`
	Ignored       int   `json:"ignored"`
	LastClientSeq int64 `json:"last_client_seq"` // app may discard everything up to and including this N
}

// ShiftResponse is a shift summary.
type ShiftResponse struct {
	ID             string     `json:"id"`
	DriverID       string     `json:"driver_id"`
	DriverName     string     `json:"driver_name,omitempty"`
	DriverPhone    string     `json:"driver_phone,omitempty"`
	PlateNumber    string     `json:"plate_number,omitempty"`
	VehicleType    string     `json:"vehicle_type"`
	Status         string     `json:"status"`
	StartedAt      time.Time  `json:"started_at"`
	EndedAt        *time.Time `json:"ended_at,omitempty"`
	EndReason      string     `json:"end_reason,omitempty"`
	StartLat       float64    `json:"start_lat"`
	StartLng       float64    `json:"start_lng"`
	EndLat         *float64   `json:"end_lat,omitempty"`
	EndLng         *float64   `json:"end_lng,omitempty"`
	PointCount     int64      `json:"point_count"`
	MockPointCount int64      `json:"mock_point_count"`
	DistanceKM     float64    `json:"distance_km"`
	LastPointAt    *time.Time `json:"last_point_at,omitempty"`
}

// TrackPoint is a point as shown to an admin.
type TrackPoint struct {
	Seq        int64     `json:"seq"`
	Lat        float64   `json:"lat"`
	Lng        float64   `json:"lng"`
	AccuracyM  float32   `json:"accuracy_m"`
	SpeedMps   float32   `json:"speed_mps"`
	Heading    float32   `json:"heading"`
	IsMock     bool      `json:"is_mock"`
	RecordedAt time.Time `json:"recorded_at"`
	Hash       string    `json:"hash"`
}

// ChainVerification is the outcome of re-computing a shift's hash chain.
type ChainVerification struct {
	Valid         bool   `json:"valid"`
	PointsChecked int64  `json:"points_checked"`
	FirstBadSeq   *int64 `json:"first_bad_seq,omitempty"`
	Problem       string `json:"problem,omitempty"`
}

// ShiftDetailResponse is a shift with its trail and integrity check.
type ShiftDetailResponse struct {
	Shift        ShiftResponse     `json:"shift"`
	Points       []TrackPoint      `json:"points"`
	Truncated    bool              `json:"truncated"`
	Verification ChainVerification `json:"verification"`
}

// TripRouteResponse is the recorded route of a trip (the driver's trail
// between match and completion/cancellation).
type TripRouteResponse struct {
	TripID     string       `json:"trip_id"`
	Status     string       `json:"status"`
	DriverID   string       `json:"driver_id,omitempty"`
	DriverName string       `json:"driver_name,omitempty"`
	PickupLat  float64      `json:"pickup_lat"`
	PickupLng  float64      `json:"pickup_lng"`
	DestLat    float64      `json:"destination_lat"`
	DestLng    float64      `json:"destination_lng"`
	MatchedAt  *time.Time   `json:"matched_at,omitempty"`
	PickedUpAt *time.Time   `json:"picked_up_at,omitempty"`
	EndedAt    *time.Time   `json:"ended_at,omitempty"`
	Points     []TrackPoint `json:"points"`
	DistanceKM float64      `json:"distance_km"`
}
