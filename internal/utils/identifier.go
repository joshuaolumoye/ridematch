package utils

import (
	"errors"
	"net/mail"
	"strings"
)

// Channel identifies which kind of identifier a user authenticated with —
// a login/registration is always exactly one or the other, never both at
// once.
type Channel string

const (
	ChannelPhone Channel = "phone"
	ChannelEmail Channel = "email"
)

// ErrInvalidEmail is returned when an identifier that looks like an email
// address doesn't parse as a valid one.
var ErrInvalidEmail = errors.New("invalid email address")

// ErrInvalidIdentifier is returned when a raw identifier is empty or
// doesn't resemble either a phone number or an email address at all.
var ErrInvalidIdentifier = errors.New("enter a valid Nigerian phone number or email address")

// NormalizeEmail validates an email address using the standard library's
// RFC 5322 parser and returns it lowercased, so "Josh@Example.com" and
// "josh@example.com" are always treated as the same account.
func NormalizeEmail(raw string) (string, error) {
	cleaned := strings.TrimSpace(raw)
	addr, err := mail.ParseAddress(cleaned)
	if err != nil || addr.Address == "" {
		return "", ErrInvalidEmail
	}
	// mail.ParseAddress accepts "Name <addr>" forms; a client sending a
	// bare address gets Name == "", and either way we only want the bare
	// address back.
	return strings.ToLower(addr.Address), nil
}

// LooksLikeEmail is a cheap heuristic used to route a raw identifier to
// the right normalizer before we know for certain it's valid.
func LooksLikeEmail(raw string) bool {
	return strings.Contains(raw, "@")
}

// NormalizeIdentifier detects whether raw is a phone number or an email
// address and normalizes it accordingly, reporting which channel it is so
// the caller knows how to deliver the OTP and which User column to key
// off of.
func NormalizeIdentifier(raw string) (identifier string, channel Channel, err error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", "", ErrInvalidIdentifier
	}

	if LooksLikeEmail(trimmed) {
		email, err := NormalizeEmail(trimmed)
		if err != nil {
			return "", "", err
		}
		return email, ChannelEmail, nil
	}

	phone, err := NormalizePhone(trimmed)
	if err != nil {
		return "", "", err
	}
	return phone, ChannelPhone, nil
}
