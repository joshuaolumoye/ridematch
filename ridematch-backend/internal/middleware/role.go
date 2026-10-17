package middleware

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"ridematch-backend/internal/utils"
)

// RequireRole returns middleware that rejects the request unless the
// authenticated caller's role is one of the given allowed roles. Must be
// mounted after RequireAuth, which populates ContextRole.
func RequireRole(allowed ...string) gin.HandlerFunc {
	allowedSet := make(map[string]struct{}, len(allowed))
	for _, r := range allowed {
		allowedSet[r] = struct{}{}
	}

	return func(c *gin.Context) {
		role, _ := c.Get(ContextRole)
		roleStr, _ := role.(string)

		if _, ok := allowedSet[roleStr]; !ok {
			utils.Fail(c, http.StatusForbidden, "you do not have permission to perform this action")
			c.Abort()
			return
		}
		c.Next()
	}
}
