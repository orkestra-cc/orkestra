// MaxMind backend — pending library integration.
//
// This file is the plug point for the real GeoIP resolver backed by
// MaxMind's GeoLite2-City DB via github.com/oschwald/geoip2-golang.
// It's currently a stub so C4 can ship its scorer factor, Haversine
// math, and test harness without blocking on the go.mod/go.sum churn
// that adding a dependency requires.
//
// To activate, in a follow-up commit:
//
//  1. Add the library to backend/go.mod:
//       github.com/oschwald/geoip2-golang
//  2. Run `go mod tidy` to populate go.sum.
//  3. Replace the body of newMaxMindResolver below with:
//
//       db, err := geoip2.Open(path)
//       if err != nil { return nil, fmt.Errorf("geoip: open %s: %w", path, err) }
//       return &maxMindResolver{db: db, logger: logger}, nil
//
//     and implement maxMindResolver.Lookup around db.City(net.ParseIP(ip))
//     mapping record.Country.IsoCode + record.City.Names["en"] +
//     record.Location.{Latitude,Longitude} into a *Location.
//     Close() should call db.Close().
//
// The scorer and all consumers bind to the Resolver interface, so no
// other file needs to change when the swap happens.
package geoip

import (
	"errors"
	"log/slog"
)

// errMaxMindUnavailable signals "the MaxMind backend file exists but
// the library isn't linked in". FromEnv catches this specifically and
// logs the remediation step rather than propagating a cryptic error
// to the auth module boot path.
var errMaxMindUnavailable = errors.New("maxmind backend not compiled in — add github.com/oschwald/geoip2-golang to go.mod")

// newMaxMindResolver is the real constructor when the library is
// linked. Today it returns errMaxMindUnavailable so FromEnv falls
// back to NoopResolver with a warn log.
func newMaxMindResolver(_ string, _ *slog.Logger) (Resolver, error) {
	return nil, errMaxMindUnavailable
}
