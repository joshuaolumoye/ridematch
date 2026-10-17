package utils

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// ErrInvalidShareToken covers every way a live-location share link can
// fail to resolve: malformed, tampered with, or past its expiry. Kept
// deliberately generic in its wording (never "expired" vs "invalid"
// specifically) so a link can't be used to probe for valid-but-expired
// trip IDs.
var ErrInvalidShareToken = errors.New("this live location link is invalid or has expired")

// GenerateShareToken creates an opaque, stateless, time-limited token
// that encodes a trip ID — no database row needed to create or revoke
// one; it simply stops working once its own embedded expiry passes.
// Anyone holding the resulting link (sent over WhatsApp, SMS, email — it
// doesn't matter which) can read that trip's public tracking view for as
// long as the token is valid, and nothing more.
func GenerateShareToken(secret, tripID string, ttl time.Duration) (token string, expiresAt time.Time) {
	expiresAt = time.Now().Add(ttl)
	payload := fmt.Sprintf("trip:%s:%d", tripID, expiresAt.Unix())
	token = base64.RawURLEncoding.EncodeToString([]byte(payload)) + "." + signShareToken(secret, payload)
	return token, expiresAt
}

// ParseShareTokenWithExpiry verifies a token's signature and expiry and
// returns the trip ID it encodes along with that expiry, so a caller can
// echo "expires_at" back without having to re-derive it elsewhere.
func ParseShareTokenWithExpiry(secret, token string) (tripID string, expiresAt time.Time, err error) {
	encodedPayload, sig, ok := strings.Cut(token, ".")
	if !ok {
		return "", time.Time{}, ErrInvalidShareToken
	}
	payloadBytes, err := base64.RawURLEncoding.DecodeString(encodedPayload)
	if err != nil {
		return "", time.Time{}, ErrInvalidShareToken
	}
	payload := string(payloadBytes)
	if !hmac.Equal([]byte(signShareToken(secret, payload)), []byte(sig)) {
		return "", time.Time{}, ErrInvalidShareToken
	}

	segs := strings.SplitN(payload, ":", 3)
	if len(segs) != 3 || segs[0] != "trip" {
		return "", time.Time{}, ErrInvalidShareToken
	}
	expiryUnix, err := strconv.ParseInt(segs[2], 10, 64)
	if err != nil {
		return "", time.Time{}, ErrInvalidShareToken
	}
	expiresAt = time.Unix(expiryUnix, 0)
	if time.Now().After(expiresAt) {
		return "", time.Time{}, ErrInvalidShareToken
	}
	return segs[1], expiresAt, nil
}

// ParseShareToken is ParseShareTokenWithExpiry without the expiry, for a
// caller that only needs the trip ID.
func ParseShareToken(secret, token string) (tripID string, err error) {
	tripID, _, err = ParseShareTokenWithExpiry(secret, token)
	return tripID, err
}

func signShareToken(secret, payload string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(payload))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}
