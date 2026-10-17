package sms

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

const termiiSendURL = "https://api.ng.termii.com/api/sms/send"

// TermiiSender sends SMS via Termii, a Nigeria-focused SMS gateway with
// reliable local delivery. Used in production once TERMII_API_KEY is set;
// see internal/sms provider selection in cmd/api/main.go.
type TermiiSender struct {
	apiKey   string
	senderID string
	client   *http.Client
}

// NewTermiiSender constructs a Sender backed by the Termii HTTP API.
func NewTermiiSender(apiKey, senderID string) *TermiiSender {
	return &TermiiSender{
		apiKey:   apiKey,
		senderID: senderID,
		client:   &http.Client{Timeout: 10 * time.Second},
	}
}

type termiiSendRequest struct {
	To      string `json:"to"`
	From    string `json:"from"`
	SMS     string `json:"sms"`
	Type    string `json:"type"`
	Channel string `json:"channel"`
	APIKey  string `json:"api_key"`
}

func (s *TermiiSender) Send(ctx context.Context, phone, message string) error {
	payload := termiiSendRequest{
		To:      phone,
		From:    s.senderID,
		SMS:     message,
		Type:    "plain",
		Channel: "generic",
		APIKey:  s.apiKey,
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("sms: failed to encode termii request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, termiiSendURL, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("sms: failed to build termii request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := s.client.Do(req)
	if err != nil {
		return fmt.Errorf("sms: termii request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		return fmt.Errorf("sms: termii returned status %d", resp.StatusCode)
	}
	return nil
}
