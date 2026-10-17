package repository

import (
	"context"
	"fmt"

	"github.com/redis/go-redis/v9"

	"ridematch-backend/internal/models"
)

// tripGeoKey returns the Redis key of the GEO sorted set holding open
// (status=requested) trip pickup points for one vehicle type — the
// passenger-request mirror of geo:drivers:{vehicle_type}. Together the
// two indexes are what let a driver's map show nearby passengers and a
// passenger's request reach nearby drivers, both as fast radius queries.
func tripGeoKey(vehicleType models.VehicleType) string {
	return fmt.Sprintf("geo:trips:%s", vehicleType)
}

// NearbyTrip is one result row from a nearby-open-trips geo query.
type NearbyTrip struct {
	TripID     string
	Latitude   float64
	Longitude  float64
	DistanceKM float64
}

// TripLocationRepository indexes open trip pickup points in Redis so
// drivers can query "which open requests are near me" as fast as
// passengers can query "which drivers are near me". A trip is added when
// it becomes open (status=requested) and removed the moment it leaves
// that state (matched, cancelled, or expired) — open trips are the only
// ones a driver should ever see here.
type TripLocationRepository interface {
	AddOpenTrip(ctx context.Context, tripID string, vehicleType models.VehicleType, lat, lng float64) error
	RemoveOpenTrip(ctx context.Context, tripID string, vehicleType models.VehicleType) error
	FindNearbyOpenTrips(ctx context.Context, vehicleType models.VehicleType, lat, lng, radiusKM float64, limit int) ([]NearbyTrip, error)
}

type redisTripLocationRepository struct {
	client *redis.Client
}

// NewTripLocationRepository constructs a Redis-backed TripLocationRepository.
func NewTripLocationRepository(client *redis.Client) TripLocationRepository {
	return &redisTripLocationRepository{client: client}
}

func (r *redisTripLocationRepository) AddOpenTrip(ctx context.Context, tripID string, vehicleType models.VehicleType, lat, lng float64) error {
	err := r.client.GeoAdd(ctx, tripGeoKey(vehicleType), &redis.GeoLocation{
		Name:      tripID,
		Longitude: lng,
		Latitude:  lat,
	}).Err()
	if err != nil {
		return fmt.Errorf("repository: failed to index open trip: %w", err)
	}
	return nil
}

func (r *redisTripLocationRepository) RemoveOpenTrip(ctx context.Context, tripID string, vehicleType models.VehicleType) error {
	err := r.client.ZRem(ctx, tripGeoKey(vehicleType), tripID).Err()
	if err != nil {
		return fmt.Errorf("repository: failed to de-index trip: %w", err)
	}
	return nil
}

func (r *redisTripLocationRepository) FindNearbyOpenTrips(ctx context.Context, vehicleType models.VehicleType, lat, lng, radiusKM float64, limit int) ([]NearbyTrip, error) {
	raw, err := r.client.GeoSearchLocation(ctx, tripGeoKey(vehicleType), &redis.GeoSearchLocationQuery{
		GeoSearchQuery: redis.GeoSearchQuery{
			Longitude:  lng,
			Latitude:   lat,
			Radius:     radiusKM,
			RadiusUnit: "km",
			Sort:       "ASC",
			Count:      limit,
		},
		WithCoord: true,
		WithDist:  true,
	}).Result()
	if err != nil {
		return nil, fmt.Errorf("repository: nearby trips geo search failed: %w", err)
	}

	results := make([]NearbyTrip, len(raw))
	for i, loc := range raw {
		results[i] = NearbyTrip{
			TripID:     loc.Name,
			Latitude:   loc.Latitude,
			Longitude:  loc.Longitude,
			DistanceKM: loc.Dist,
		}
	}
	return results, nil
}
