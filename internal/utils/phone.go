package utils

import (
	"errors"
	"regexp"
	"strings"
)

// ErrInvalidPhone is returned when a phone number cannot be normalized to
// a valid Nigerian E.164 number.
var ErrInvalidPhone = errors.New("invalid Nigerian phone number")

var nigerianLocalPattern = regexp.MustCompile(`^0[789][01]\d{8}$`) // e.g. 08012345678
var nigerianE164Pattern = regexp.MustCompile(`^\+234[789][01]\d{8}$`)

// NormalizePhone accepts common Nigerian phone number formats a user might
// type (080..., 234..., +234..., with spaces/dashes) and returns a strict
// E.164 form (+234XXXXXXXXXX), or ErrInvalidPhone if it doesn't look like
// a valid Nigerian mobile number.
func NormalizePhone(raw string) (string, error) {
	cleaned := strings.TrimSpace(raw)
	cleaned = strings.ReplaceAll(cleaned, " ", "")
	cleaned = strings.ReplaceAll(cleaned, "-", "")

	switch {
	case strings.HasPrefix(cleaned, "+234"):
		// already E.164-ish, fall through to validation below
	case strings.HasPrefix(cleaned, "234"):
		cleaned = "+" + cleaned
	case strings.HasPrefix(cleaned, "0"):
		if !nigerianLocalPattern.MatchString(cleaned) {
			return "", ErrInvalidPhone
		}
		cleaned = "+234" + cleaned[1:]
	default:
		return "", ErrInvalidPhone
	}

	if !nigerianE164Pattern.MatchString(cleaned) {
		return "", ErrInvalidPhone
	}
	return cleaned, nil
}
