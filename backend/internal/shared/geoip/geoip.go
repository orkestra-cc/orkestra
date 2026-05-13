// Package geoip resolves IP addresses to approximate geographic
// locations so the auth risk scorer can reason about impossible-travel
// signals. Pure interface + math in this file; concrete backends live
// in sibling files so the dependency on MaxMind can be added/removed
// without touching the consumers.
//
// Section C item #4 of the 2026-04-24 auth roadmap.
package geoip

import (
	"context"
	"log/slog"
	"math"
	"os"
)

// Location is the subset of a GeoIP record the risk scorer reads.
// Fields mirror the MaxMind GeoLite2-City layout but are
// library-agnostic so swapping the backend (MaxMind → DB-IP → HTTP
// provider) doesn't ripple through callers.
//
// Accuracy is city-level at best — the returned lat/lng is the
// centroid of the matched city or country, not the caller's actual
// position. The scorer treats large differences as signals, not
// precise coordinates.
type Location struct {
	IP        string
	Country   string  // ISO-3166-1 alpha-2, e.g. "US", "IT"
	City      string  // English city name when available; empty otherwise
	Latitude  float64 // decimal degrees, WGS84
	Longitude float64 // decimal degrees, WGS84
}

// Resolver looks up a Location by IP address. Implementations must be
// safe for concurrent use — the scorer calls Lookup from the login
// hot path. A nil Location with nil error means "no match" (reserved,
// private, loopback, or absent from the DB) — the scorer treats that
// as "no geo signal" and skips the factor.
type Resolver interface {
	Lookup(ctx context.Context, ip string) (*Location, error)
	// Close releases any backend resources (MaxMind DB file handles).
	// Safe to call multiple times. NoopResolver returns nil.
	Close() error
}

// NoopResolver is the zero-value Resolver: every lookup returns
// (nil, nil). Used when no GeoIP backend is configured — the scorer
// skips the impossible_travel factor without erroring.
type NoopResolver struct{}

func (NoopResolver) Lookup(_ context.Context, _ string) (*Location, error) { return nil, nil }
func (NoopResolver) Close() error                                          { return nil }

// FromEnv reads AUTH_GEOIP_DB_PATH and returns a Resolver. When the
// variable is unset, returns NoopResolver (GeoIP disabled — scorer
// falls back to C1's rapid_ip_change factor which catches the tight-
// window subset of impossible travel anyway). When the variable is
// set, attempts to construct a MaxMind-backed resolver; if that
// backend isn't compiled in yet, logs a loud warning with the
// remediation step and returns NoopResolver so the rest of the auth
// stack continues to work.
//
// This split lets the MaxMind library integration land as a follow-up
// commit without blocking C4's plumbing on the dependency-graph work.
// Once the real backend ships, this function dispatches to it
// unchanged.
func FromEnv(logger *slog.Logger) Resolver {
	if logger == nil {
		logger = slog.Default()
	}
	path := os.Getenv("AUTH_GEOIP_DB_PATH")
	if path == "" {
		logger.Info("geoip: disabled (AUTH_GEOIP_DB_PATH unset) — impossible_travel factor inert")
		return NoopResolver{}
	}
	if r, err := newMaxMindResolver(path, logger); err == nil {
		return r
	} else if logger != nil {
		// newMaxMindResolver returns a non-nil error when the MaxMind
		// backend isn't compiled in — callers see a clear remediation
		// message instead of a silent no-op.
		logger.Warn("geoip: AUTH_GEOIP_DB_PATH set but MaxMind backend unavailable",
			slog.String("path", path),
			slog.String("error", err.Error()),
			slog.String("remediation", "add github.com/oschwald/geoip2-golang to backend/go.mod and run `go mod tidy`; then geoip will pick up the DB automatically"))
	}
	return NoopResolver{}
}

// Distance returns the great-circle distance in kilometers between two
// Locations, using the Haversine formula. Accuracy is within ~0.5% at
// any distance — fine for "is this transoceanic?" gating.
//
// Earth radius varies by latitude; 6371 km is the canonical mean used
// in navigation. Returns 0 for nil inputs so callers can chain without
// a presence guard.
func Distance(a, b *Location) float64 {
	if a == nil || b == nil {
		return 0
	}
	const earthRadiusKm = 6371.0
	toRad := func(deg float64) float64 { return deg * math.Pi / 180 }
	lat1 := toRad(a.Latitude)
	lat2 := toRad(b.Latitude)
	dLat := toRad(b.Latitude - a.Latitude)
	dLon := toRad(b.Longitude - a.Longitude)
	h := math.Sin(dLat/2)*math.Sin(dLat/2) +
		math.Cos(lat1)*math.Cos(lat2)*math.Sin(dLon/2)*math.Sin(dLon/2)
	c := 2 * math.Atan2(math.Sqrt(h), math.Sqrt(1-h))
	return earthRadiusKm * c
}
