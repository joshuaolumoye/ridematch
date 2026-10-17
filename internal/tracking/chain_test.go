package tracking

import (
	"testing"
	"time"
)

func TestPointHashDeterministicAndChained(t *testing.T) {
	secret := []byte("k")
	ts := time.UnixMilli(1760000000123)
	h1 := PointHash(secret, GenesisHash, "s", 1, 6.5244, 3.3792, ts, false)
	h1b := PointHash(secret, GenesisHash, "s", 1, 6.52440000001, 3.3792, ts, false)
	if h1 != h1b {
		t.Fatal("hash must be stable under sub-precision float noise")
	}
	h2 := PointHash(secret, h1, "s", 2, 6.5250, 3.3800, ts.Add(time.Second), false)
	if h2 == h1 {
		t.Fatal("chain links must differ")
	}
	// Tamper: change a coordinate -> different hash.
	if PointHash(secret, GenesisHash, "s", 1, 6.5245, 3.3792, ts, false) == h1 {
		t.Fatal("coordinate change must change hash")
	}
	// Tamper: flip mock flag.
	if PointHash(secret, GenesisHash, "s", 1, 6.5244, 3.3792, ts, true) == h1 {
		t.Fatal("mock flag must be covered")
	}
	// Different key -> different hash.
	if PointHash([]byte("other"), GenesisHash, "s", 1, 6.5244, 3.3792, ts, false) == h1 {
		t.Fatal("key must matter")
	}
}

func TestHaversine(t *testing.T) {
	// Lagos Ikeja -> Victoria Island is roughly 15-20 km.
	d := HaversineMeters(6.6018, 3.3515, 6.4281, 3.4219)
	if d < 17000 || d > 23000 {
		t.Fatalf("unexpected distance %f", d)
	}
	if HaversineMeters(6.5, 3.4, 6.5, 3.4) != 0 {
		t.Fatal("same point must be 0")
	}
}
