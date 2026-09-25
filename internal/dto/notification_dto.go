package dto

// NotificationResponse is one row of a user's notifications inbox.
type NotificationResponse struct {
	ID        string `json:"id"`
	Type      string `json:"type"`
	Title     string `json:"title"`
	Body      string `json:"body"`
	Data      string `json:"data,omitempty"`
	Read      bool   `json:"read"`
	CreatedAt string `json:"created_at"`
}

// NotificationListResponse is a page of a user's notifications, plus how
// many of ALL their notifications (not just this page) are still unread
// — so the app can show an unread badge without a second round trip.
type NotificationListResponse struct {
	Items       []NotificationResponse `json:"items"`
	Pagination  Pagination             `json:"pagination"`
	UnreadCount int64                  `json:"unread_count"`
}

// UnreadNotificationCountResponse backs a lightweight polling endpoint
// for just the badge count.
type UnreadNotificationCountResponse struct {
	UnreadCount int64 `json:"unread_count"`
}

// RegisterPushTokenRequest is the payload for PATCH /users/me/push-token.
type RegisterPushTokenRequest struct {
	// ExpoPushToken is the Expo push token the app obtained from
	// Notifications.getExpoPushTokenAsync(). An empty string clears the
	// stored token (e.g. on sign-out / notifications disabled).
	ExpoPushToken string `json:"expo_push_token" binding:"omitempty,max=255"`
}
