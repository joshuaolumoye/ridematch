package middleware

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// CORS returns permissive CORS middleware suitable for a mobile-app
// backend (the React Native app doesn't send an Origin header the way a
// browser does, but this also allows the Swagger UI docs page and any
// future admin web dashboard to call the API directly).
func CORS() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("Access-Control-Allow-Origin", "*")
		c.Header("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
		c.Header("Access-Control-Allow-Headers", "Authorization, Content-Type")

		if c.Request.Method == http.MethodOptions {
			c.AbortWithStatus(http.StatusNoContent)
			return
		}
		c.Next()
	}
}
