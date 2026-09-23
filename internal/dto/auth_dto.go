// Package dto defines the request/response payload shapes for the API.
// Keeping these separate from internal/models means the database schema
// can evolve without automatically changing (or leaking fields into) the
// public API contract.
package dto

// RequestOTPRequest is the payload for POST /auth/otp/request.
type RequestOTPRequest struct {
	Phone string `json:"phone" binding:"required" example:"08012345678"`
}

// RequestOTPResponse confirms an OTP was sent and tells the client how
// long until it may request another one.
type RequestOTPResponse struct {
	Phone            string `json:"phone" example:"+2348012345678"`
	ExpiresInSeconds int    `json:"expires_in_seconds" example:"300"`
	ResendInSeconds  int    `json:"resend_in_seconds" example:"60"`
}

// VerifyOTPRequest is the payload for POST /auth/otp/verify. The same
// endpoint handles both first-time registration and subsequent login: if
// no account exists for the phone number yet, one is created.
type VerifyOTPRequest struct {
	Phone string `json:"phone" binding:"required" example:"08012345678"`
	Code  string `json:"code" binding:"required,len=6" example:"048213"`
	// Name is only used the first time a phone number registers; ignored
	// on subsequent logins.
	Name string `json:"name" binding:"omitempty,max=120" example:"Joshua Olumoye"`
}

// AuthTokens is the token pair issued on successful authentication.
type AuthTokens struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresAt    string `json:"expires_at" example:"2026-09-23T12:00:00Z"`
	TokenType    string `json:"token_type" example:"Bearer"`
}

// VerifyOTPResponse is returned on successful OTP verification.
type VerifyOTPResponse struct {
	User      UserResponse `json:"user"`
	Tokens    AuthTokens   `json:"tokens"`
	IsNewUser bool         `json:"is_new_user"`
}

// RefreshTokenRequest is the payload for POST /auth/token/refresh.
type RefreshTokenRequest struct {
	RefreshToken string `json:"refresh_token" binding:"required"`
}

// LogoutRequest is the payload for POST /auth/logout.
type LogoutRequest struct {
	RefreshToken string `json:"refresh_token" binding:"required"`
}

// UpdateProfileRequest is the payload for PATCH /users/me. Both fields are
// optional — a client sends only what it changed; a field omitted from
// the JSON body leaves that column untouched (see AuthService.UpdateProfile).
type UpdateProfileRequest struct {
	Name     *string `json:"name" binding:"omitempty,min=2,max=120"`
	PhotoURL *string `json:"photo_url" binding:"omitempty,url"`
}

// UserResponse is the public-safe representation of a User returned by
// the API.
type UserResponse struct {
	ID            string  `json:"id"`
	Phone         string  `json:"phone"`
	Name          string  `json:"name"`
	PhotoURL      string  `json:"photo_url,omitempty"`
	Role          string  `json:"role"`
	Status        string  `json:"status"`
	RatingAverage float64 `json:"rating_average"`
	IsDriver      bool    `json:"is_driver"`
}
