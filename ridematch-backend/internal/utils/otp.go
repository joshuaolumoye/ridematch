package utils

import (
	"crypto/rand"
	"fmt"
	"math/big"

	"golang.org/x/crypto/bcrypt"
)

// GenerateOTP produces a random numeric one-time code of the given length
// (e.g. length=6 -> "048213"), using crypto/rand so codes aren't
// predictable. Leading zeros are preserved.
func GenerateOTP(length int) (string, error) {
	if length <= 0 {
		length = 6
	}
	max := new(big.Int)
	max.Exp(big.NewInt(10), big.NewInt(int64(length)), nil)

	n, err := rand.Int(rand.Reader, max)
	if err != nil {
		return "", fmt.Errorf("utils: failed to generate OTP: %w", err)
	}

	return fmt.Sprintf("%0*d", length, n), nil
}

// HashOTP hashes a plaintext OTP code with bcrypt so the database never
// stores a usable code, mirroring how passwords are normally handled.
func HashOTP(code string) (string, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(code), bcrypt.DefaultCost)
	if err != nil {
		return "", fmt.Errorf("utils: failed to hash OTP: %w", err)
	}
	return string(hash), nil
}

// CompareOTP reports whether a plaintext code matches a previously hashed
// one.
func CompareOTP(hash, code string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(code)) == nil
}
