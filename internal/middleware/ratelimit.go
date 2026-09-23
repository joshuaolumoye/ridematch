package middleware

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"

	"ridematch-backend/internal/utils"
)

// RateLimit returns middleware enforcing a fixed-window request limit per
// authenticated user per action (e.g. "trip.create", "trip.offer") —
// cheap abuse protection against a driver or passenger spamming trip
// requests/offers. Must run after RequireAuth, which populates
// ContextUserID.
//
// Implementation: a single Redis INCR + conditional EXPIRE per request —
// O(1), sub-millisecond in practice, so the limiter itself never becomes
// the latency bottleneck it's meant to protect against.
func RateLimit(client *redis.Client, action string, limit int, window time.Duration) gin.HandlerFunc {
	return func(c *gin.Context) {
		userID := UserIDFromContext(c)
		if userID == "" {
			// No authenticated user to key on — RequireAuth should have
			// already rejected the request, but fail closed just in case
			// middleware ordering ever changes.
			utils.Fail(c, http.StatusUnauthorized, "authentication required")
			c.Abort()
			return
		}

		key := fmt.Sprintf("ratelimit:%s:%s", action, userID)

		ctx, cancel := context.WithTimeout(c.Request.Context(), 500*time.Millisecond)
		defer cancel()

		count, err := client.Incr(ctx, key).Result()
		if err != nil {
			// Redis being unreachable shouldn't take down the whole API —
			// fail open on infrastructure errors, not on the caller.
			c.Next()
			return
		}
		if count == 1 {
			client.Expire(ctx, key, window)
		}

		if count > int64(limit) {
			ttl, _ := client.TTL(ctx, key).Result()
			c.Header("Retry-After", fmt.Sprintf("%.0f", ttl.Seconds()))
			utils.Fail(c, http.StatusTooManyRequests, "too many requests, please slow down")
			c.Abort()
			return
		}

		c.Next()
	}
}
