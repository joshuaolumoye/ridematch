package models

import "time"

// NotificationType categorizes a notification so the mobile app can pick
// an icon/deep link for it, and so future filtering ("show me only
// document notices") has something to filter on.
type NotificationType string

const (
	NotificationDriverDocs    NotificationType = "driver_docs"    // admin asking a driver to update/resubmit a document
	NotificationSubscription  NotificationType = "subscription"   // subscription activated/expiring
	NotificationTrip          NotificationType = "trip"           // trip lifecycle (matched, cancelled, etc.)
	NotificationAccountStatus NotificationType = "account_status" // suspended/reactivated/banned
	NotificationSystem        NotificationType = "system"         // general admin/broadcast message
)

// Notification is an in-app message shown to one user (rider, driver, or
// admin) in their notifications inbox — persisted so it survives being
// offline when it was sent, in addition to (not instead of) an
// immediate WebSocket push and a phone push notification. REST is the
// source of truth; the other two channels are best-effort delivery.
type Notification struct {
	Base

	UserID string           `gorm:"type:char(36);index;not null" json:"user_id"`
	Type   NotificationType `gorm:"type:varchar(30);not null" json:"type"`
	Title  string           `gorm:"type:varchar(150);not null" json:"title"`
	Body   string           `gorm:"type:varchar(1000);not null" json:"body"`

	// Data carries an optional small JSON payload for deep-linking (e.g.
	// {"trip_id": "..."} or {"driver_id": "..."}) — stored as raw text,
	// the mobile app parses it, the backend never inspects it.
	Data string `gorm:"type:varchar(500)" json:"data,omitempty"`

	ReadAt *time.Time `json:"read_at,omitempty"`
}

func (Notification) TableName() string {
	return "notifications"
}
