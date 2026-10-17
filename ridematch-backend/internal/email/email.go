// Package email abstracts sending transactional email behind a single
// interface, mirroring internal/sms — the auth service never depends on a
// specific provider. Swap providers by changing EMAIL_PROVIDER in config;
// no service-layer code changes needed.
package email

import (
	"context"
	"fmt"
	"log"
)

// Sender delivers an email to an address.
type Sender interface {
	Send(ctx context.Context, to, subject, body string) error
}

// ConsoleSender logs the message instead of sending it. This is the
// default for local development so OTP codes are visible in the server
// logs without needing real SMTP credentials configured.
type ConsoleSender struct{}

// NewConsoleSender constructs a Sender that logs to stdout.
func NewConsoleSender() *ConsoleSender {
	return &ConsoleSender{}
}

func (s *ConsoleSender) Send(_ context.Context, to, subject, body string) error {
	log.Printf("[EMAIL -> %s] %s\n%s", to, subject, body)
	return nil
}

// BuildOTPEmail formats the standard OTP email subject and body.
func BuildOTPEmail(code string, ttlMinutes int) (subject, body string) {
	subject = "Your RideMatch verification code"
	body = fmt.Sprintf(
		"Your RideMatch verification code is %s.\n\nIt expires in %d minutes. Do not share this code with anyone — RideMatch staff will never ask you for it.",
		code, ttlMinutes,
	)
	return subject, body
}
