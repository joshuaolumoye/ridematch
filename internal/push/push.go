// Package push abstracts sending a mobile push notification behind a
// single interface, mirroring internal/sms and internal/email — the
// notification service never depends on a specific provider. RideMatch's
// app is Expo-managed, so the only provider today is Expo's push service,
// which itself fans out to APNs/FCM; there is no per-platform code here.
package push

import (
	"context"
	"log"
)

// Sender delivers a push notification to a device identified by an Expo
// push token (e.g. "ExponentPushToken[xxxxxxxx]"). data is an optional
// small payload the app can read on tap (e.g. {"trip_id": "..."}) — nil
// is fine when there's nothing to deep-link to.
type Sender interface {
	Send(ctx context.Context, expoPushToken, title, body string, data map[string]string) error
}

// ConsoleSender logs the push instead of sending it. Default for local
// development so pushes are visible in server logs without needing a
// real device/EAS project configured.
type ConsoleSender struct{}

// NewConsoleSender constructs a Sender that logs to stdout.
func NewConsoleSender() *ConsoleSender {
	return &ConsoleSender{}
}

func (s *ConsoleSender) Send(_ context.Context, expoPushToken, title, body string, data map[string]string) error {
	log.Printf("[PUSH -> %s] %s: %s %v", expoPushToken, title, body, data)
	return nil
}
