// Package middleware holds gin middleware: JWT authentication, CORS, and
// request logging/recovery configuration.
package middleware

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"ridematch-backend/internal/utils"
)

// Context keys used to pass authenticated-user data from middleware to
// handlers. Using typed constants (not raw strings) avoids collisions.
const (
	ContextUserID = "auth_user_id"
	ContextPhone  = "auth_phone"
	ContextEmail  = "auth_email"
	ContextRole   = "auth_role"
)

// RequireAuth returns middleware that rejects the request unless it carries
// a valid `Authorization: Bearer <token>` access token, and otherwise
// populates the gin context with the caller's identity for handlers to use.
func RequireAuth(jwtManager *utils.JWTManager) gin.HandlerFunc {
	return func(c *gin.Context) {
		header := c.GetHeader("Authorization")
		if header == "" {
			utils.Fail(c, http.StatusUnauthorized, "missing authorization header")
			c.Abort()
			return
		}

		parts := strings.SplitN(header, " ", 2)
		if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
			utils.Fail(c, http.StatusUnauthorized, "authorization header must be in the form: Bearer <token>")
			c.Abort()
			return
		}

		claims, err := jwtManager.ParseAccessToken(parts[1])
		if err != nil {
			utils.Fail(c, http.StatusUnauthorized, "invalid or expired access token")
			c.Abort()
			return
		}

		c.Set(ContextUserID, claims.UserID)
		c.Set(ContextPhone, claims.Phone)
		c.Set(ContextEmail, claims.Email)
		c.Set(ContextRole, string(claims.Role))
		c.Next()
	}
}

// UserIDFromContext retrieves the authenticated caller's user ID. Only
// valid on routes behind RequireAuth.
func UserIDFromContext(c *gin.Context) string {
	id, _ := c.Get(ContextUserID)
	str, _ := id.(string)
	return str
}
