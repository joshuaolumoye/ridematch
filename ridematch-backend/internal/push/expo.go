package push

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

const expoPushSendURL = "https://exp.host/--/api/v2/push/send"

// ExpoSender sends push notifications via Expo's push service — the
// standard delivery path for an Expo-managed app, which forwards to
// APNs (iOS) or FCM (Android) on RideMatch's behalf. No per-platform
// credentials are needed here; Expo holds those.
type ExpoSender struct {
	accessToken string // optional: Expo's "enhanced security" push access token
	client      *http.Client
}

// NewExpoSender constructs a Sender backed by Expo's push API. accessToken
// may be empty — Expo's push endpoint works without one; it's only
// required if the project has "Enhanced Push Security" enabled.
func NewExpoSender(accessToken string) *ExpoSender {
	return &ExpoSender{
		accessToken: accessToken,
		client:      &http.Client{Timeout: 10 * time.Second},
	}
}

type expoPushMessage struct {
	To    string            `json:"to"`
	Title string            `json:"title"`
	Body  string            `json:"body"`
	Data  map[string]string `json:"data,omitempty"`
	Sound string            `json:"sound,omitempty"`
}

func (s *ExpoSender) Send(ctx context.Context, expoPushToken, title, body string, data map[string]string) error {
	if expoPushToken == "" {
		return nil
	}

	payload, err := json.Marshal(expoPushMessage{
		To:    expoPushToken,
		Title: title,
		Body:  body,
		Data:  data,
		Sound: "default",
	})
	if err != nil {
		return fmt.Errorf("push: failed to encode expo push message: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, expoPushSendURL, bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("push: failed to build expo push request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	if s.accessToken != "" {
		req.Header.Set("Authorization", "Bearer "+s.accessToken)
	}

	resp, err := s.client.Do(req)
	if err != nil {
		return fmt.Errorf("push: expo push request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		return fmt.Errorf("push: expo push service returned status %d", resp.StatusCode)
	}
	return nil
}
