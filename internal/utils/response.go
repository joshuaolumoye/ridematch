// Package utils holds small, dependency-light helpers shared across the
// service and handler layers: JSON response envelopes, JWT issuance, OTP
// generation/hashing, and phone number normalization.
package utils

import "github.com/gin-gonic/gin"

// APIResponse is the single response envelope every endpoint returns, so
// API consumers (the React Native app) can rely on one consistent shape
// whether a call succeeds or fails.
type APIResponse struct {
	Success bool        `json:"success"`
	Message string      `json:"message,omitempty"`
	Data    interface{} `json:"data,omitempty"`
	Error   string      `json:"error,omitempty"`
	Meta    interface{} `json:"meta,omitempty"`
}

// Success writes a 2xx JSON response with the given status code, message
// and payload. Call this from a handler instead of gin.Context.JSON
// directly so every success response has an identical shape.
func Success(c *gin.Context, status int, message string, data interface{}) {
	c.JSON(status, APIResponse{
		Success: true,
		Message: message,
		Data:    data,
	})
}

// SuccessWithMeta is Success plus a meta block (pagination info, etc.).
func SuccessWithMeta(c *gin.Context, status int, message string, data, meta interface{}) {
	c.JSON(status, APIResponse{
		Success: true,
		Message: message,
		Data:    data,
		Meta:    meta,
	})
}

// Fail writes an error JSON response with the given status code and
// message. Use this for all handler-level errors instead of hand-rolling
// gin.H{...} so error responses stay consistent for API consumers.
func Fail(c *gin.Context, status int, message string) {
	c.JSON(status, APIResponse{
		Success: false,
		Error:   message,
	})
}
