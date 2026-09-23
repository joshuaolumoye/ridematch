package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"ridematch-backend/internal/dto"
	"ridematch-backend/internal/middleware"
	"ridematch-backend/internal/service"
	"ridematch-backend/internal/utils"
)

// AuthHandler exposes phone + OTP authentication endpoints.
type AuthHandler struct {
	auth *service.AuthService
}

// NewAuthHandler constructs an AuthHandler.
func NewAuthHandler(auth *service.AuthService) *AuthHandler {
	return &AuthHandler{auth: auth}
}

// RequestOTP godoc
//
//	@Summary		Request a login/registration OTP
//	@Description	Sends a 6-digit one-time code by SMS to the given Nigerian phone number. Used for both first-time registration and subsequent logins — the same endpoint covers both. Subject to a resend cooldown (default 60s) per phone number.
//	@Tags			Auth
//	@Accept			json
//	@Produce		json
//	@Param			request	body		dto.RequestOTPRequest	true	"Phone number to send the code to"
//	@Success		200		{object}	utils.APIResponse{data=dto.RequestOTPResponse}
//	@Failure		400		{object}	utils.APIResponse	"Invalid phone number format"
//	@Failure		429		{object}	utils.APIResponse	"Resend cooldown still active"
//	@Router			/auth/otp/request [post]
func (h *AuthHandler) RequestOTP(c *gin.Context) {
	var req dto.RequestOTPRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.Fail(c, http.StatusBadRequest, "phone is required")
		return
	}

	resp, err := h.auth.RequestOTP(c.Request.Context(), req.Phone)
	if err != nil {
		handleServiceError(c, err)
		return
	}

	utils.Success(c, http.StatusOK, "verification code sent", resp)
}

// VerifyOTP godoc
//
//	@Summary		Verify an OTP and obtain access/refresh tokens
//	@Description	Verifies the 6-digit code sent to a phone number. If no account exists for the phone number yet, one is created automatically (pass `name` on first verification). Returns a JWT access token and an opaque refresh token.
//	@Tags			Auth
//	@Accept			json
//	@Produce		json
//	@Param			request	body		dto.VerifyOTPRequest	true	"Phone, code, and optional name for new accounts"
//	@Success		200		{object}	utils.APIResponse{data=dto.VerifyOTPResponse}
//	@Failure		400		{object}	utils.APIResponse	"Missing/invalid fields, incorrect or expired code"
//	@Failure		403		{object}	utils.APIResponse	"Account suspended or banned"
//	@Failure		429		{object}	utils.APIResponse	"Too many incorrect attempts"
//	@Router			/auth/otp/verify [post]
func (h *AuthHandler) VerifyOTP(c *gin.Context) {
	var req dto.VerifyOTPRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.Fail(c, http.StatusBadRequest, "phone and a 6-digit code are required")
		return
	}

	resp, err := h.auth.VerifyOTP(c.Request.Context(), req.Phone, req.Code, req.Name)
	if err != nil {
		handleServiceError(c, err)
		return
	}

	message := "logged in successfully"
	if resp.IsNewUser {
		message = "account created successfully"
	}
	utils.Success(c, http.StatusOK, message, resp)
}

// RefreshToken godoc
//
//	@Summary		Exchange a refresh token for a new token pair
//	@Description	Rotates the refresh token: the submitted token is revoked and a new access + refresh token pair is issued. The old refresh token cannot be reused.
//	@Tags			Auth
//	@Accept			json
//	@Produce		json
//	@Param			request	body		dto.RefreshTokenRequest	true	"Refresh token"
//	@Success		200		{object}	utils.APIResponse{data=dto.AuthTokens}
//	@Failure		401		{object}	utils.APIResponse	"Invalid, expired, or already-used refresh token"
//	@Router			/auth/token/refresh [post]
func (h *AuthHandler) RefreshToken(c *gin.Context) {
	var req dto.RefreshTokenRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.Fail(c, http.StatusBadRequest, "refresh_token is required")
		return
	}

	tokens, err := h.auth.RefreshToken(c.Request.Context(), req.RefreshToken)
	if err != nil {
		handleServiceError(c, err)
		return
	}

	utils.Success(c, http.StatusOK, "token refreshed", tokens)
}

// Logout godoc
//
//	@Summary		Log out (revoke a refresh token)
//	@Description	Revokes the given refresh token, ending that session. Other devices/sessions for the same user are unaffected.
//	@Tags			Auth
//	@Accept			json
//	@Produce		json
//	@Param			request	body		dto.LogoutRequest	true	"Refresh token to revoke"
//	@Success		200		{object}	utils.APIResponse
//	@Router			/auth/logout [post]
func (h *AuthHandler) Logout(c *gin.Context) {
	var req dto.LogoutRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.Fail(c, http.StatusBadRequest, "refresh_token is required")
		return
	}

	if err := h.auth.Logout(c.Request.Context(), req.RefreshToken); err != nil {
		handleServiceError(c, err)
		return
	}

	utils.Success(c, http.StatusOK, "logged out successfully", nil)
}

// Me godoc
//
//	@Summary		Get the authenticated user's profile
//	@Description	Returns the profile of the user identified by the access token.
//	@Tags			Users
//	@Produce		json
//	@Security		BearerAuth
//	@Success		200	{object}	utils.APIResponse{data=dto.UserResponse}
//	@Failure		401	{object}	utils.APIResponse
//	@Router			/users/me [get]
func (h *AuthHandler) Me(c *gin.Context) {
	userID := middleware.UserIDFromContext(c)

	resp, err := h.auth.Me(c.Request.Context(), userID)
	if err != nil {
		handleServiceError(c, err)
		return
	}

	utils.Success(c, http.StatusOK, "profile fetched", resp)
}

// UpdateProfile godoc
//
//	@Summary		Update the authenticated user's profile
//	@Description	Updates the caller's own name and/or photo. Both fields are optional and independent — send only what changed.
//	@Tags			Users
//	@Accept			json
//	@Produce		json
//	@Security		BearerAuth
//	@Param			request	body		dto.UpdateProfileRequest	true	"Fields to change"
//	@Success		200		{object}	utils.APIResponse{data=dto.UserResponse}
//	@Failure		400		{object}	utils.APIResponse
//	@Router			/users/me [patch]
func (h *AuthHandler) UpdateProfile(c *gin.Context) {
	var req dto.UpdateProfileRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.Fail(c, http.StatusBadRequest, "name must be 2-120 characters and photo_url, if given, must be a valid URL")
		return
	}

	resp, err := h.auth.UpdateProfile(c.Request.Context(), middleware.UserIDFromContext(c), req)
	if err != nil {
		handleServiceError(c, err)
		return
	}

	utils.Success(c, http.StatusOK, "profile updated", resp)
}

// DeleteAccount godoc
//
//	@Summary		Delete the authenticated user's account
//	@Description	Permanently ends every session for this account and soft-deletes it. Identifying details (phone, name, photo) are scrubbed immediately; trip/rating history is preserved for the other party's records. This cannot be undone from the app.
//	@Tags			Users
//	@Produce		json
//	@Security		BearerAuth
//	@Success		200	{object}	utils.APIResponse
//	@Router			/users/me [delete]
func (h *AuthHandler) DeleteAccount(c *gin.Context) {
	if err := h.auth.DeleteAccount(c.Request.Context(), middleware.UserIDFromContext(c)); err != nil {
		handleServiceError(c, err)
		return
	}
	utils.Success(c, http.StatusOK, "account deleted", nil)
}
