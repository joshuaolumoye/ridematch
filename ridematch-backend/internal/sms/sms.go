// Package sms abstracts sending SMS messages behind a single interface so
// the auth service never depends on a specific provider. Swap providers by
// changing SMS_PROVIDER in config — no service-layer code changes needed.
package sms

import (
	"context"
	"fmt"
	"log"
)

// Sender delivers a text message to a phone number (E.164 format).
type Sender interface {
	Send(ctx context.Context, phone, message string) error
}

// ConsoleSender logs the message instead of sending it. This is the
// default for local development so OTP codes are visible in the server
// logs without needing a paid SMS account configured.
type ConsoleSender struct{}

// NewConsoleSender constructs a Sender that logs to stdout.
func NewConsoleSender() *ConsoleSender {
	return &ConsoleSender{}
}

func (s *ConsoleSender) Send(_ context.Context, phone, message string) error {
	log.Printf("[SMS -> %s] %s", phone, message)
	return nil
}

// BuildOTPMessage formats the standard OTP SMS body.
func BuildOTPMessage(code string, ttlMinutes int) string {
	return fmt.Sprintf("Your RideMatch verification code is %s. It expires in %d minutes. Do not share this code.", code, ttlMinutes)
}
