package models

import "time"

// ShiftStatus is the lifecycle of a driver shift.
type ShiftStatus string

const (
	ShiftActive ShiftStatus = "active"
	ShiftEnded  ShiftStatus = "ended"
)

// ShiftEndReason records why a shift closed — part of the evidence trail.
type ShiftEndReason string

const (
	ShiftEndDriverOffline ShiftEndReason = "driver_offline"
	ShiftEndStale         ShiftEndReason = "stale_no_signal"
	ShiftEndSuspended     ShiftEndReason = "account_restricted"
	ShiftEndReplaced      ShiftEndReason = "replaced_by_new_shift"
)

// DriverShift is one continuous "online" session of a driver: where they
// started work, where they ended, and (via LocationPoint) every position in
// between. It is the unit admins review as evidence.
type DriverShift struct {
	ImmutableBase

	DriverID    string      `gorm:"type:char(36);index:idx_shift_driver_started,priority:1;not null" json:"driver_id"`
	UserID      string      `gorm:"type:char(36);index;not null" json:"user_id"`
	VehicleType VehicleType `gorm:"type:varchar(20);not null" json:"vehicle_type"`
	Status      ShiftStatus `gorm:"type:varchar(10);index;not null" json:"status"`

	StartedAt time.Time  `gorm:"type:datetime(3);index:idx_shift_driver_started,priority:2;not null" json:"started_at"`
	EndedAt   *time.Time `json:"ended_at,omitempty"`
	EndReason string     `gorm:"type:varchar(30)" json:"end_reason,omitempty"`

	StartLat float64  `gorm:"type:decimal(10,7)" json:"start_lat"`
	StartLng float64  `gorm:"type:decimal(10,7)" json:"start_lng"`
	EndLat   *float64 `gorm:"type:decimal(10,7)" json:"end_lat,omitempty"`
	EndLng   *float64 `gorm:"type:decimal(10,7)" json:"end_lng,omitempty"`

	PointCount     int64      `json:"point_count"`
	MockPointCount int64      `json:"mock_point_count"`
	DistanceMeters float64    `json:"distance_meters"`
	LastPointAt    *time.Time `gorm:"index" json:"last_point_at,omitempty"`
	LastSeq        int64      `json:"-"`
	LastClientSeq  int64      `json:"-"` // highest app-side counter ingested; makes retried batches idempotent
	LastHash       string     `gorm:"type:char(64)" json:"-"`
	LastLat        float64    `gorm:"type:decimal(10,7)" json:"-"`
	LastLng        float64    `gorm:"type:decimal(10,7)" json:"-"`
}

func (DriverShift) TableName() string { return "driver_shifts" }

// LocationPoint is one GPS sample in a shift's trail. Each point carries an
// HMAC over its own content and the previous point's hash, so removing,
// inserting or editing any row breaks the chain from that point on
// (verified by TrackingService.VerifyShift).
type LocationPoint struct {
	ID         uint64    `gorm:"primaryKey;autoIncrement" json:"-"`
	ShiftID    string    `gorm:"type:char(36);not null;uniqueIndex:idx_point_shift_seq,priority:1" json:"shift_id"`
	Seq        int64     `gorm:"not null;uniqueIndex:idx_point_shift_seq,priority:2" json:"seq"`
	DriverID   string    `gorm:"type:char(36);index:idx_point_driver_time,priority:1;not null" json:"driver_id"`
	Lat        float64   `gorm:"type:decimal(10,7);not null" json:"lat"`
	Lng        float64   `gorm:"type:decimal(10,7);not null" json:"lng"`
	AccuracyM  float32   `json:"accuracy_m"`
	SpeedMps   float32   `json:"speed_mps"`
	Heading    float32   `json:"heading"`
	IsMock     bool      `json:"is_mock"`
	RecordedAt time.Time `gorm:"type:datetime(3);index:idx_point_driver_time,priority:2;not null" json:"recorded_at"`
	ReceivedAt time.Time `gorm:"type:datetime(3);not null" json:"received_at"`
	Hash       string    `gorm:"type:char(64);not null" json:"hash"`
}

func (LocationPoint) TableName() string { return "location_points" }
