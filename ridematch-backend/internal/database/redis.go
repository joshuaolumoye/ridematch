package database

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/redis/go-redis/v9"

	"ridematch-backend/internal/config"
)

// NewRedis opens a Redis client used for OTP storage, live driver
// geolocation (GEO commands), and rate limiting.
func NewRedis(cfg *config.Config) (*redis.Client, error) {
	client := redis.NewClient(&redis.Options{
		Addr:     cfg.RedisAddr,
		Password: cfg.RedisPassword,
		DB:       cfg.RedisDB,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := client.Ping(ctx).Err(); err != nil {
		return nil, fmt.Errorf("database: failed to connect to redis: %w", err)
	}

	log.Println("database: redis connection established")
	return client, nil
}
