// Package tracking holds the pure, dependency-free pieces of driver GPS
// evidence: the tamper-evident hash chain and distance maths.
package tracking

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math"
	"time"
)

// GenesisHash is the "previous hash" of the first point in a shift.
const GenesisHash = "genesis"

// Round7 rounds a coordinate to 7 decimal places (~1cm), the precision the
// database stores — hashing the rounded value keeps hashes reproducible
// after a round trip through DECIMAL(10,7).
func Round7(v float64) float64 { return math.Round(v*1e7) / 1e7 }

// PointHash computes the HMAC-SHA256 chain link for one location point.
func PointHash(secret []byte, prev, shiftID string, seq int64, lat, lng float64, recordedAt time.Time, mock bool) string {
	msg := fmt.Sprintf("%s|%s|%d|%.7f|%.7f|%d|%t", prev, shiftID, seq, Round7(lat), Round7(lng), recordedAt.UnixMilli(), mock)
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(msg))
	return hex.EncodeToString(mac.Sum(nil))
}

// HaversineMeters is the great-circle distance between two coordinates.
func HaversineMeters(lat1, lng1, lat2, lng2 float64) float64 {
	const r = 6371000.0
	toRad := func(d float64) float64 { return d * math.Pi / 180 }
	dLat := toRad(lat2 - lat1)
	dLng := toRad(lng2 - lng1)
	a := math.Sin(dLat/2)*math.Sin(dLat/2) +
		math.Cos(toRad(lat1))*math.Cos(toRad(lat2))*math.Sin(dLng/2)*math.Sin(dLng/2)
	return 2 * r * math.Asin(math.Min(1, math.Sqrt(a)))
}
