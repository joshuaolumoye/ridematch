package utils

import (
	"crypto/sha256"
	"encoding/hex"
)

// HashToken returns a hex-encoded SHA-256 digest of a raw token string.
// Refresh tokens are long, high-entropy random values (unlike short OTP
// codes), so a fast, deterministic hash is appropriate here — bcrypt would
// add cost with no security benefit for a value that's never guessed.
func HashToken(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}
