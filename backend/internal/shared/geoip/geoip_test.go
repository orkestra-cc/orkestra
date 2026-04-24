package geoip

import (
	"context"
	"math"
	"testing"
)

func TestDistance_KnownPairs(t *testing.T) {
	// NYC (40.7128, -74.0060) → London (51.5074, -0.1278): ~5570 km
	nyc := &Location{Latitude: 40.7128, Longitude: -74.0060}
	london := &Location{Latitude: 51.5074, Longitude: -0.1278}
	got := Distance(nyc, london)
	if math.Abs(got-5570) > 50 {
		t.Errorf("NYC→London distance: got %.0f km, want ~5570 km", got)
	}

	// NYC → Tokyo (35.6762, 139.6503): ~10,850 km
	tokyo := &Location{Latitude: 35.6762, Longitude: 139.6503}
	got = Distance(nyc, tokyo)
	if math.Abs(got-10850) > 100 {
		t.Errorf("NYC→Tokyo distance: got %.0f km, want ~10850 km", got)
	}

	// Same point
	got = Distance(nyc, nyc)
	if got > 0.01 {
		t.Errorf("same-point distance should be ~0, got %.4f km", got)
	}

	// Antipodes (0,0) ↔ (0,180): half earth circumference ~20,015 km
	a := &Location{Latitude: 0, Longitude: 0}
	b := &Location{Latitude: 0, Longitude: 180}
	got = Distance(a, b)
	if math.Abs(got-20015) > 50 {
		t.Errorf("antipode distance: got %.0f km, want ~20015 km", got)
	}
}

func TestDistance_NilSafe(t *testing.T) {
	if d := Distance(nil, nil); d != 0 {
		t.Errorf("nil,nil should be 0, got %v", d)
	}
	loc := &Location{Latitude: 40, Longitude: -74}
	if d := Distance(loc, nil); d != 0 {
		t.Errorf("loc,nil should be 0, got %v", d)
	}
	if d := Distance(nil, loc); d != 0 {
		t.Errorf("nil,loc should be 0, got %v", d)
	}
}

func TestNoopResolver_AlwaysNil(t *testing.T) {
	r := NoopResolver{}
	loc, err := r.Lookup(context.Background(), "8.8.8.8")
	if err != nil {
		t.Errorf("noop Lookup should never error, got %v", err)
	}
	if loc != nil {
		t.Errorf("noop Lookup should return nil location, got %+v", loc)
	}
	if err := r.Close(); err != nil {
		t.Errorf("noop Close should never error, got %v", err)
	}
}

func TestFromEnv_NoPathReturnsNoop(t *testing.T) {
	t.Setenv("AUTH_GEOIP_DB_PATH", "")
	r := FromEnv(nil)
	if _, ok := r.(NoopResolver); !ok {
		t.Errorf("missing path should return NoopResolver, got %T", r)
	}
}

func TestFromEnv_PathSetFallsBackWithWarn(t *testing.T) {
	// With the MaxMind backend not compiled in (current state), setting
	// the path triggers a warn-log fallback to NoopResolver so the auth
	// boot path doesn't panic. Once newMaxMindResolver is implemented,
	// this test becomes "returns MaxMind resolver" — update in lockstep.
	t.Setenv("AUTH_GEOIP_DB_PATH", "/tmp/fake.mmdb")
	r := FromEnv(nil)
	if _, ok := r.(NoopResolver); !ok {
		t.Errorf("MaxMind backend pending: path-set should still fall back to NoopResolver, got %T", r)
	}
}
