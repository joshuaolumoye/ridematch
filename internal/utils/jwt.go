package utils

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"ridematch-backend/internal/models"
)

// ErrInvalidToken is returned when a JWT fails signature verification,
// has expired, or is otherwise malformed.
var ErrInvalidToken = errors.New("utils: invalid or expired token")

// AccessClaims is the payload embedded in a short-lived access token.
// It carries just enough to authorize a request without a database
// round-trip on every call.
type AccessClaims struct {
	UserID string          `json:"uid"`
	Phone  string          `json:"phone"`
	Role   models.UserRole `json:"role"`
	jwt.RegisteredClaims
}

// JWTManager issues and verifies access tokens, and generates the opaque
// random string used as a refresh token.
type JWTManager struct {
	accessSecret []byte
	accessTTL    time.Duration
	refreshTTL   time.Duration
	issuer       string
}

// NewJWTManager constructs a JWTManager from configuration secrets/TTLs.
func NewJWTManager(accessSecret string, accessTTL, refreshTTL time.Duration) *JWTManager {
	return &JWTManager{
		accessSecret: []byte(accessSecret),
		accessTTL:    accessTTL,
		refreshTTL:   refreshTTL,
		issuer:       "ridematch-api",
	}
}

// GenerateAccessToken issues a signed JWT for the given user, valid for
// the configured access-token TTL.
func (m *JWTManager) GenerateAccessToken(user *models.User) (string, time.Time, error) {
	expiresAt := time.Now().Add(m.accessTTL)

	claims := AccessClaims{
		UserID: user.ID,
		Phone:  user.Phone,
		Role:   user.Role,
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    m.issuer,
			Subject:   user.ID,
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			ExpiresAt: jwt.NewNumericDate(expiresAt),
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := token.SignedString(m.accessSecret)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("utils: failed to sign access token: %w", err)
	}
	return signed, expiresAt, nil
}

// ParseAccessToken validates a signed access token and returns its claims.
func (m *JWTManager) ParseAccessToken(tokenString string) (*AccessClaims, error) {
	claims := &AccessClaims{}

	token, err := jwt.ParseWithClaims(tokenString, claims, func(t *jwt.Token) (interface{}, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
		}
		return m.accessSecret, nil
	})
	if err != nil || !token.Valid {
		return nil, ErrInvalidToken
	}
	return claims, nil
}

// GenerateRefreshToken returns a new cryptographically random opaque
// token (not a JWT) plus its expiry. Refresh tokens are deliberately
// opaque and stored server-side (hashed) so they can be revoked
// individually — a JWT refresh token cannot be revoked without an
// additional denylist, which defeats the purpose of using a JWT for it.
func (m *JWTManager) GenerateRefreshToken() (string, time.Time, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", time.Time{}, fmt.Errorf("utils: failed to generate refresh token: %w", err)
	}
	raw := base64.RawURLEncoding.EncodeToString(b)
	return raw, time.Now().Add(m.refreshTTL), nil
}

// RefreshTTL exposes the configured refresh-token lifetime.
func (m *JWTManager) RefreshTTL() time.Duration {
	return m.refreshTTL
}
