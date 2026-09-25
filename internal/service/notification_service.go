package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"ridematch-backend/internal/dto"
	"ridematch-backend/internal/models"
	"ridematch-backend/internal/push"
	"ridematch-backend/internal/repository"
	"ridematch-backend/internal/ws"
)

// NotificationService creates and delivers a user's in-app
// notifications. Every notification goes out on up to three channels:
//  1. Persisted to the database (the source of truth — GET /notifications
//     always works, online or not).
//  2. Pushed instantly over the user's live WebSocket connection, if any
//     (for an in-app toast/badge update while the app is open).
//  3. Sent as a phone push notification via Expo, if the account has a
//     registered push token (so it reaches them even if the app isn't
//     open — the notification-tray experience).
//
// The DB write is the only one that must succeed; the other two are
// best-effort exactly like every other push in this codebase (WS events,
// SMS, email) — a dropped push is never the reason a notification is
// "lost", because it's still sitting in the list.
type NotificationService struct {
	notifications repository.NotificationRepository
	users         repository.UserRepository
	hub           *ws.Hub
	pushSender    push.Sender
}

// NewNotificationService constructs a NotificationService.
func NewNotificationService(
	notifications repository.NotificationRepository,
	users repository.UserRepository,
	hub *ws.Hub,
	pushSender push.Sender,
) *NotificationService {
	return &NotificationService{
		notifications: notifications,
		users:         users,
		hub:           hub,
		pushSender:    pushSender,
	}
}

// SendToUser creates a notification for a user and best-effort delivers
// it over WebSocket and phone push. data is an optional small
// deep-linking payload (e.g. map[string]string{"trip_id": tripID}); pass
// nil when there's nothing to link to.
func (s *NotificationService) SendToUser(ctx context.Context, userID string, notifType models.NotificationType, title, body string, data map[string]string) (*dto.NotificationResponse, error) {
	var dataJSON string
	if len(data) > 0 {
		encoded, err := json.Marshal(data)
		if err == nil {
			dataJSON = string(encoded)
		}
	}

	notification := &models.Notification{
		UserID: userID,
		Type:   notifType,
		Title:  title,
		Body:   body,
		Data:   dataJSON,
	}
	if err := s.notifications.Create(ctx, notification); err != nil {
		return nil, fmt.Errorf("service: failed to create notification: %w", err)
	}

	resp := toNotificationResponse(notification)

	// Best-effort real-time delivery — never fails the call.
	s.hub.SendToUser(userID, ws.Event{Type: ws.EventNotificationNew, Data: resp})

	if user, err := s.users.FindByID(ctx, userID); err == nil && user.PushToken != "" {
		_ = s.pushSender.Send(ctx, user.PushToken, title, body, data)
	}

	return &resp, nil
}

// List returns a page of a user's notifications, newest first, plus how
// many are unread across all of them.
func (s *NotificationService) List(ctx context.Context, userID string, page, pageSize int) (*dto.NotificationListResponse, error) {
	page, pageSize = normalizePage(page, pageSize)

	notifications, total, err := s.notifications.FindByUser(ctx, userID, pageSize, (page-1)*pageSize)
	if err != nil {
		return nil, fmt.Errorf("service: failed to list notifications: %w", err)
	}
	unread, err := s.notifications.CountUnread(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("service: failed to count unread notifications: %w", err)
	}

	items := make([]dto.NotificationResponse, 0, len(notifications))
	for _, n := range notifications {
		items = append(items, toNotificationResponse(&n))
	}

	return &dto.NotificationListResponse{
		Items:       items,
		Pagination:  dto.NewPagination(page, pageSize, total),
		UnreadCount: unread,
	}, nil
}

// UnreadCount is a lightweight call for just the badge number.
func (s *NotificationService) UnreadCount(ctx context.Context, userID string) (int64, error) {
	return s.notifications.CountUnread(ctx, userID)
}

// MarkRead marks one notification read. Marking an already-read
// notification read again is a no-op success, not an error.
func (s *NotificationService) MarkRead(ctx context.Context, notificationID, userID string) error {
	if err := s.notifications.MarkRead(ctx, notificationID, userID); err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return ErrNotificationNotFound
		}
		return fmt.Errorf("service: failed to mark notification read: %w", err)
	}
	return nil
}

// MarkAllRead marks every one of a user's unread notifications read.
func (s *NotificationService) MarkAllRead(ctx context.Context, userID string) error {
	if err := s.notifications.MarkAllRead(ctx, userID); err != nil {
		return fmt.Errorf("service: failed to mark all notifications read: %w", err)
	}
	return nil
}

func toNotificationResponse(n *models.Notification) dto.NotificationResponse {
	return dto.NotificationResponse{
		ID:        n.ID,
		Type:      string(n.Type),
		Title:     n.Title,
		Body:      n.Body,
		Data:      n.Data,
		Read:      n.ReadAt != nil,
		CreatedAt: n.CreatedAt.UTC().Format(time.RFC3339),
	}
}
