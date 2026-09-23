package repository

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"

	"ridematch-backend/internal/models"
)

// geoKey returns the Redis key of the GEO sorted set holding live driver
// positions for one vehicle type. Partitioning by vehicle type means a
// passenger asking for nearby okada riders never pays the cost of
// scanning car positions.
func geoKey(vehicleType models.VehicleType) string {
	return fmt.Sprintf("geo:drivers:%s", vehicleType)
}

// lastSeenKey is a single hash (driverID -> unix millis) shared across all
// vehicle types, used to filter out drivers whose last location ping is
// too old to trust even though they were never explicitly taken offline
// (app killed, connection dropped, etc).
const lastSeenKey = "driver:lastseen"

// NearbyDriver is one result row from a nearby-driver geo query.
type NearbyDriver struct {
	DriverID   string
	Latitude   float64
	Longitude  float64
	DistanceKM float64
}

// LocationRepository defines live driver geolocation operations. Unlike
// the other repositories this is Redis-backed, not MySQL/GORM-backed —
// live position is inherently ephemeral, ill-suited to a durable table,
// and Redis GEO commands are purpose-built for radius queries.
type LocationRepository interface {
	// UpdateLocation records (or updates) a driver's live position and
	// refreshes their last-seen timestamp. Called both when a driver goes
	// online and on every subsequent location ping while online.
	UpdateLocation(ctx context.Context, driverID string, vehicleType models.VehicleType, lat, lng float64) error

	// RemoveLocation removes a driver from the live geo index (going
	// offline, or lazily evicted for being stale).
	RemoveLocation(ctx context.Context, driverID string, vehicleType models.VehicleType) error

	// FindNearby returns up to `limit` drivers of the given vehicle type
	// within `radiusKM` of (lat, lng), nearest first, excluding any whose
	// last-seen timestamp is older than `staleAfter`. Stale entries found
	// during the scan are lazily evicted from the geo index.
	FindNearby(ctx context.Context, vehicleType models.VehicleType, lat, lng, radiusKM float64, limit int, staleAfter time.Duration) ([]NearbyDriver, error)
}

type redisLocationRepository struct {
	client *redis.Client
}

// NewLocationRepository constructs a Redis-backed LocationRepository.
func NewLocationRepository(client *redis.Client) LocationRepository {
	return &redisLocationRepository{client: client}
}

func (r *redisLocationRepository) UpdateLocation(ctx context.Context, driverID string, vehicleType models.VehicleType, lat, lng float64) error {
	pipe := r.client.TxPipeline()

	pipe.GeoAdd(ctx, geoKey(vehicleType), &redis.GeoLocation{
		Name:      driverID,
		Longitude: lng,
		Latitude:  lat,
	})
	pipe.HSet(ctx, lastSeenKey, driverID, time.Now().UnixMilli())

	if _, err := pipe.Exec(ctx); err != nil {
		return fmt.Errorf("repository: failed to update driver location: %w", err)
	}
	return nil
}

func (r *redisLocationRepository) RemoveLocation(ctx context.Context, driverID string, vehicleType models.VehicleType) error {
	pipe := r.client.TxPipeline()
	pipe.ZRem(ctx, geoKey(vehicleType), driverID)
	pipe.HDel(ctx, lastSeenKey, driverID)

	if _, err := pipe.Exec(ctx); err != nil {
		return fmt.Errorf("repository: failed to remove driver location: %w", err)
	}
	return nil
}

func (r *redisLocationRepository) FindNearby(ctx context.Context, vehicleType models.VehicleType, lat, lng, radiusKM float64, limit int, staleAfter time.Duration) ([]NearbyDriver, error) {
	// Over-fetch a little so that filtering out stale entries still
	// leaves close to `limit` fresh results in the common case.
	fetchCount := limit * 2
	if fetchCount < limit+10 {
		fetchCount = limit + 10
	}

	raw, err := r.client.GeoSearchLocation(ctx, geoKey(vehicleType), &redis.GeoSearchLocationQuery{
		GeoSearchQuery: redis.GeoSearchQuery{
			Longitude:  lng,
			Latitude:   lat,
			Radius:     radiusKM,
			RadiusUnit: "km",
			Sort:       "ASC",
			Count:      fetchCount,
		},
		WithCoord: true,
		WithDist:  true,
	}).Result()
	if err != nil {
		return nil, fmt.Errorf("repository: geo search failed: %w", err)
	}
	if len(raw) == 0 {
		return []NearbyDriver{}, nil
	}

	ids := make([]string, len(raw))
	for i, loc := range raw {
		ids[i] = loc.Name
	}

	lastSeen, err := r.client.HMGet(ctx, lastSeenKey, ids...).Result()
	if err != nil {
		return nil, fmt.Errorf("repository: failed to check driver staleness: %w", err)
	}

	cutoff := time.Now().Add(-staleAfter).UnixMilli()

	results := make([]NearbyDriver, 0, len(raw))
	for i, loc := range raw {
		fresh := false
		if lastSeen[i] != nil {
			if ms, ok := parseUnixMillis(lastSeen[i]); ok && ms >= cutoff {
				fresh = true
			}
		}

		if !fresh {
			// Lazily evict: this driver pinged once but went silent
			// (crashed, lost connection) without an explicit "go
			// offline" call. Best-effort cleanup — a failure here just
			// means it gets filtered again on the next query.
			_ = r.RemoveLocation(ctx, ids[i], vehicleType)
			continue
		}

		results = append(results, NearbyDriver{
			DriverID:   loc.Name,
			Latitude:   loc.Latitude,
			Longitude:  loc.Longitude,
			DistanceKM: loc.Dist,
		})

		if len(results) >= limit {
			break
		}
	}

	return results, nil
}

// parseUnixMillis converts an HMGET result (interface{} holding a string)
// into an int64 millisecond timestamp.
func parseUnixMillis(v interface{}) (int64, bool) {
	s, ok := v.(string)
	if !ok {
		return 0, false
	}
	var ms int64
	if _, err := fmt.Sscanf(s, "%d", &ms); err != nil {
		return 0, false
	}
	return ms, true
}
