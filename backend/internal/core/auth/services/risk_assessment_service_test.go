package services

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/orkestra/backend/internal/core/auth/models"
	"github.com/orkestra/backend/internal/core/auth/repository"
	"github.com/orkestra/backend/internal/shared/geoip"
)

// stubSessionRepo satisfies repository.AuthSessionRepository. The three
// methods the risk scorer consumes return configurable values; every
// other method panics so accidental use surfaces as a loud test failure
// instead of a silent zero value.
type stubSessionRepo struct {
	mostRecent *models.AuthSessionDoc
	mostErr    error

	fpCount int64
	fpErr   error

	ipCount int64
	ipErr   error
}

func (s *stubSessionRepo) GetMostRecentSessionByUser(_ context.Context, _ string) (*models.AuthSessionDoc, error) {
	return s.mostRecent, s.mostErr
}

func (s *stubSessionRepo) CountSessionsByUserAndFingerprint(_ context.Context, _, _ string, _ time.Time) (int64, error) {
	return s.fpCount, s.fpErr
}

func (s *stubSessionRepo) CountSessionsByUserAndIP(_ context.Context, _, _ string, _ time.Time) (int64, error) {
	return s.ipCount, s.ipErr
}

// Unused — panic on accidental use.
func (s *stubSessionRepo) CreateSession(context.Context, *models.AuthSessionDoc) error {
	panic("unexpected CreateSession")
}
func (s *stubSessionRepo) GetByUUID(context.Context, string) (*models.AuthSessionDoc, error) {
	panic("unexpected GetByUUID")
}
func (s *stubSessionRepo) GetByUserAndDevice(context.Context, string, string) (*models.AuthSessionDoc, error) {
	panic("unexpected GetByUserAndDevice")
}
func (s *stubSessionRepo) GetActiveSessionsByUser(context.Context, string) ([]*models.AuthSessionDoc, error) {
	panic("unexpected GetActiveSessionsByUser")
}
func (s *stubSessionRepo) UpdateLastActivity(context.Context, string) error {
	panic("unexpected UpdateLastActivity")
}
func (s *stubSessionRepo) UpdateRiskScore(context.Context, string, float64, string) error {
	panic("unexpected UpdateRiskScore")
}
func (s *stubSessionRepo) AddSecurityEvent(context.Context, string, *models.SecurityEventLog) error {
	panic("unexpected AddSecurityEvent")
}
func (s *stubSessionRepo) UpdateDeviceInfo(context.Context, string, *models.DeviceInfo) error {
	panic("unexpected UpdateDeviceInfo")
}
func (s *stubSessionRepo) TerminateSession(context.Context, string) error {
	panic("unexpected TerminateSession")
}
func (s *stubSessionRepo) TerminateSessionByDevice(context.Context, string, string) error {
	panic("unexpected TerminateSessionByDevice")
}
func (s *stubSessionRepo) TerminateAllUserSessions(context.Context, string) error {
	panic("unexpected TerminateAllUserSessions")
}
func (s *stubSessionRepo) TerminateExpiredSessions(context.Context) (int64, error) {
	panic("unexpected TerminateExpiredSessions")
}
func (s *stubSessionRepo) DeleteAllByUser(context.Context, string) (int64, error) {
	panic("unexpected DeleteAllByUser")
}
func (s *stubSessionRepo) GetSessionStats(context.Context, string) (*repository.SessionStats, error) {
	panic("unexpected GetSessionStats")
}
func (s *stubSessionRepo) GetActiveDevices(context.Context, string) ([]*repository.DeviceSession, error) {
	panic("unexpected GetActiveDevices")
}
func (s *stubSessionRepo) GetSessionsByLocation(context.Context, string, string) ([]*models.AuthSessionDoc, error) {
	panic("unexpected GetSessionsByLocation")
}
func (s *stubSessionRepo) GetHighRiskSessions(context.Context, float64) ([]*models.AuthSessionDoc, error) {
	panic("unexpected GetHighRiskSessions")
}
func (s *stubSessionRepo) RenameDevice(context.Context, string, string, string) error {
	panic("unexpected RenameDevice")
}
func (s *stubSessionRepo) GetDeviceSessionHistory(context.Context, string, string, int) ([]*models.AuthSessionDoc, error) {
	panic("unexpected GetDeviceSessionHistory")
}
func (s *stubSessionRepo) GetRecentSecurityEvents(context.Context, string, string, time.Time) ([]*models.SecurityEventLog, error) {
	panic("unexpected GetRecentSecurityEvents")
}
func (s *stubSessionRepo) GetSuspiciousSessions(context.Context, string) ([]*models.AuthSessionDoc, error) {
	panic("unexpected GetSuspiciousSessions")
}

// Compile-time guard that the stub really satisfies the repository
// interface. If a new method is added upstream the test file won't
// compile — a loud reminder to extend the stub.
var _ repository.AuthSessionRepository = (*stubSessionRepo)(nil)

// newScorer constructs a scorer with the stub repo and a pinned clock so
// rapid_ip_change assertions don't depend on wall time. GeoIP is
// NoopResolver by default — use newScorerWithGeo for impossible-travel
// tests.
func newScorer(t *testing.T, repo *stubSessionRepo, now time.Time) *riskAssessmentService {
	t.Helper()
	svc := NewRiskAssessmentService(repo, nil).(*riskAssessmentService)
	svc.clock = func() time.Time { return now }
	return svc
}

// stubGeoResolver returns canned Locations keyed on IP. An IP that
// isn't in the map returns (nil, nil) — "no match", scorer skips the
// factor.
type stubGeoResolver struct {
	locs map[string]*geoip.Location
	err  error
}

func (s *stubGeoResolver) Lookup(_ context.Context, ip string) (*geoip.Location, error) {
	if s.err != nil {
		return nil, s.err
	}
	return s.locs[ip], nil
}
func (s *stubGeoResolver) Close() error { return nil }

func newScorerWithGeo(t *testing.T, repo *stubSessionRepo, resolver geoip.Resolver, velocityKmh float64, now time.Time) *riskAssessmentService {
	t.Helper()
	svc := NewRiskAssessmentServiceWithGeoIP(repo, resolver, velocityKmh, nil).(*riskAssessmentService)
	svc.clock = func() time.Time { return now }
	return svc
}

// --- tests ---

func TestRiskLevelForScore(t *testing.T) {
	cases := map[float64]string{
		0.0:  RiskLevelLow,
		0.29: RiskLevelLow,
		0.3:  RiskLevelMedium,
		0.49: RiskLevelMedium,
		0.5:  RiskLevelHigh,
		0.69: RiskLevelHigh,
		0.7:  RiskLevelCritical,
		1.0:  RiskLevelCritical,
	}
	for in, want := range cases {
		if got := RiskLevelForScore(in); got != want {
			t.Errorf("RiskLevelForScore(%v) = %q, want %q", in, got, want)
		}
	}
}

func TestAssessLoginRisk_ZeroScoreOnFirstLogin(t *testing.T) {
	// No prior session → first-ever login → every factor is inert.
	now := time.Date(2026, 4, 25, 12, 0, 0, 0, time.UTC)
	scorer := newScorer(t, &stubSessionRepo{mostRecent: nil}, now)
	got, err := scorer.AssessLoginRisk(context.Background(), "u1", &models.SecurityContext{
		IPAddress:   "8.8.8.8",
		Fingerprint: "fp-abc",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Score != 0.0 {
		t.Errorf("first login score: want 0.0, got %v", got.Score)
	}
	if got.Level != RiskLevelLow {
		t.Errorf("first login level: want %q, got %q", RiskLevelLow, got.Level)
	}
	if len(got.Factors) != 0 {
		t.Errorf("first login should have no factors, got %+v", got.Factors)
	}
}

func TestAssessLoginRisk_ZeroScoreWhenCtxNil(t *testing.T) {
	scorer := newScorer(t, &stubSessionRepo{}, time.Now())
	got, err := scorer.AssessLoginRisk(context.Background(), "u1", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Score != 0.0 {
		t.Errorf("nil ctx score: want 0.0, got %v", got.Score)
	}
}

func TestAssessLoginRisk_NewFingerprintAndIP(t *testing.T) {
	// User has prior history but this fingerprint + IP are both new.
	// Expected factors: new_device_fingerprint (0.3) + new_ip (0.2) = 0.5
	// → high.
	now := time.Date(2026, 4, 25, 12, 0, 0, 0, time.UTC)
	repo := &stubSessionRepo{
		mostRecent: &models.AuthSessionDoc{
			UUID:      "prior-session",
			UserUUID:  "u1",
			IPAddress: "10.0.0.5", // different but not within rapid window
			CreatedAt: now.Add(-48 * time.Hour),
		},
		fpCount: 0, // new fingerprint
		ipCount: 0, // new IP
	}
	scorer := newScorer(t, repo, now)
	got, err := scorer.AssessLoginRisk(context.Background(), "u1", &models.SecurityContext{
		IPAddress:   "8.8.8.8",
		Fingerprint: "fp-new",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := WeightNewDeviceFingerprint + WeightNewIP
	if got.Score != want {
		t.Errorf("score: want %v, got %v", want, got.Score)
	}
	if got.Level != RiskLevelHigh {
		t.Errorf("level: want %q, got %q", RiskLevelHigh, got.Level)
	}
	if len(got.Factors) != 2 {
		t.Fatalf("expected 2 factors, got %d: %+v", len(got.Factors), got.Factors)
	}
	if got.Factors[0].Details["factor"] != "new_device_fingerprint" {
		t.Errorf("factor[0] wrong: %+v", got.Factors[0])
	}
	if got.Factors[1].Details["factor"] != "new_ip" {
		t.Errorf("factor[1] wrong: %+v", got.Factors[1])
	}
}

func TestAssessLoginRisk_RapidIPChangeAllThreeFactors(t *testing.T) {
	// Prior session started 2 minutes ago from a different IP, and both
	// the current IP and fingerprint are new. All three factors fire:
	// 0.3 + 0.2 + 0.4 = 0.9 → critical.
	now := time.Date(2026, 4, 25, 12, 0, 0, 0, time.UTC)
	repo := &stubSessionRepo{
		mostRecent: &models.AuthSessionDoc{
			UserUUID:  "u1",
			IPAddress: "10.0.0.5",
			CreatedAt: now.Add(-2 * time.Minute),
		},
		fpCount: 0,
		ipCount: 0,
	}
	scorer := newScorer(t, repo, now)
	got, _ := scorer.AssessLoginRisk(context.Background(), "u1", &models.SecurityContext{
		IPAddress:   "8.8.8.8",
		Fingerprint: "fp-new",
	})
	want := WeightNewDeviceFingerprint + WeightNewIP + WeightRapidIPChange
	if got.Score != want {
		t.Errorf("score: want %v, got %v", want, got.Score)
	}
	if got.Level != RiskLevelCritical {
		t.Errorf("level: want %q, got %q", RiskLevelCritical, got.Level)
	}
	if len(got.Factors) != 3 {
		t.Fatalf("expected 3 factors, got %d", len(got.Factors))
	}
	if got.Factors[2].Details["factor"] != "rapid_ip_change" {
		t.Errorf("factor[2] wrong: %+v", got.Factors[2])
	}
}

func TestAssessLoginRisk_RapidSameIPDoesNotFire(t *testing.T) {
	// Prior session 2m ago from the SAME IP → tab refresh / race. Not
	// a risk signal. Known fingerprint + IP → zero score.
	now := time.Date(2026, 4, 25, 12, 0, 0, 0, time.UTC)
	repo := &stubSessionRepo{
		mostRecent: &models.AuthSessionDoc{
			UserUUID:  "u1",
			IPAddress: "8.8.8.8",
			CreatedAt: now.Add(-2 * time.Minute),
		},
		fpCount: 4,
		ipCount: 12,
	}
	scorer := newScorer(t, repo, now)
	got, _ := scorer.AssessLoginRisk(context.Background(), "u1", &models.SecurityContext{
		IPAddress:   "8.8.8.8",
		Fingerprint: "fp-known",
	})
	if got.Score != 0.0 {
		t.Errorf("same-IP rapid should not fire: got %+v", got)
	}
}

func TestAssessLoginRisk_RapidDifferentIPOutsideWindowDoesNotFire(t *testing.T) {
	// Prior session 15 minutes ago from a different IP → outside the
	// 5-minute rapid window. Only new_ip (0.2) fires because the IP is
	// new to the user.
	now := time.Date(2026, 4, 25, 12, 0, 0, 0, time.UTC)
	repo := &stubSessionRepo{
		mostRecent: &models.AuthSessionDoc{
			UserUUID:  "u1",
			IPAddress: "10.0.0.5",
			CreatedAt: now.Add(-15 * time.Minute),
		},
		fpCount: 4, // known fingerprint
		ipCount: 0, // new IP
	}
	scorer := newScorer(t, repo, now)
	got, _ := scorer.AssessLoginRisk(context.Background(), "u1", &models.SecurityContext{
		IPAddress:   "8.8.8.8",
		Fingerprint: "fp-known",
	})
	if got.Score != WeightNewIP {
		t.Errorf("outside rapid window: want %v, got %v (factors=%+v)", WeightNewIP, got.Score, got.Factors)
	}
}

func TestAssessLoginRisk_EmptyFingerprintSkipsFactor(t *testing.T) {
	// Login path doesn't supply a fingerprint (e.g. early mobile client).
	// new_device_fingerprint cannot fire; new_ip still does.
	now := time.Date(2026, 4, 25, 12, 0, 0, 0, time.UTC)
	repo := &stubSessionRepo{
		mostRecent: &models.AuthSessionDoc{
			UserUUID:  "u1",
			IPAddress: "10.0.0.5",
			CreatedAt: now.Add(-24 * time.Hour),
		},
		ipCount: 0,
	}
	scorer := newScorer(t, repo, now)
	got, _ := scorer.AssessLoginRisk(context.Background(), "u1", &models.SecurityContext{
		IPAddress: "8.8.8.8",
		// Fingerprint intentionally empty
	})
	if got.Score != WeightNewIP {
		t.Errorf("missing fingerprint: want %v, got %v", WeightNewIP, got.Score)
	}
}

func TestAssessLoginRisk_RepoErrorFallsBackToZero(t *testing.T) {
	// A Mongo error on the prior-session lookup must not block login.
	// The scorer logs a warn and returns zero score so the login path
	// continues with the pre-C1 default.
	now := time.Date(2026, 4, 25, 12, 0, 0, 0, time.UTC)
	repo := &stubSessionRepo{mostErr: errors.New("mongo: timeout")}
	scorer := newScorer(t, repo, now)
	got, err := scorer.AssessLoginRisk(context.Background(), "u1", &models.SecurityContext{
		IPAddress: "8.8.8.8",
	})
	if err != nil {
		t.Fatalf("scorer must not propagate repo errors: %v", err)
	}
	if got.Score != 0.0 {
		t.Errorf("score on repo error: want 0.0, got %v", got.Score)
	}
}

// ----- impossible_travel factor (Section C item #4) -----

// Canonical coordinates used across the travel tests. NYC → Tokyo is
// a natural "impossibly fast in 1 hour" pair; the short-hop test uses
// inline coords.
var (
	locNYC   = &geoip.Location{IP: "8.8.8.8", Country: "US", City: "New York", Latitude: 40.7128, Longitude: -74.0060}
	locTokyo = &geoip.Location{IP: "1.1.1.1", Country: "JP", City: "Tokyo", Latitude: 35.6762, Longitude: 139.6503}
)

func TestAssessLoginRisk_ImpossibleTravelFires(t *testing.T) {
	// Prior session in NYC 1 hour ago; current login in Tokyo. Distance
	// ~10,850 km in 1h = 10,850 km/h, way over the 1000 km/h threshold.
	now := time.Date(2026, 4, 25, 12, 0, 0, 0, time.UTC)
	repo := &stubSessionRepo{
		mostRecent: &models.AuthSessionDoc{
			UserUUID:  "u1",
			IPAddress: locNYC.IP,
			CreatedAt: now.Add(-1 * time.Hour),
		},
		fpCount: 4, // known fp — isolate the geo factor
		ipCount: 4, // known IP — isolate the geo factor
	}
	resolver := &stubGeoResolver{locs: map[string]*geoip.Location{
		locNYC.IP:   locNYC,
		locTokyo.IP: locTokyo,
	}}
	scorer := newScorerWithGeo(t, repo, resolver, DefaultImpossibleTravelVelocityKmh, now)
	got, err := scorer.AssessLoginRisk(context.Background(), "u1", &models.SecurityContext{
		IPAddress:   locTokyo.IP,
		Fingerprint: "fp-known",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Score != WeightImpossibleTravel {
		t.Errorf("score: want %v, got %v (factors=%+v)", WeightImpossibleTravel, got.Score, got.Factors)
	}
	if got.Level != RiskLevelHigh {
		t.Errorf("level: want high (impossible_travel alone is 0.5), got %q", got.Level)
	}
	if len(got.Factors) != 1 || got.Factors[0].Details["factor"] != "impossible_travel" {
		t.Fatalf("expected single impossible_travel factor, got %+v", got.Factors)
	}
	// Sanity-check the factor details carry useful context for audit.
	if got.Factors[0].Details["priorCountry"] != "US" || got.Factors[0].Details["currentCountry"] != "JP" {
		t.Errorf("details missing country info: %+v", got.Factors[0].Details)
	}
}

func TestAssessLoginRisk_ImpossibleTravelIgnoresRealisticFlight(t *testing.T) {
	// NYC → Tokyo in 12 hours = ~900 km/h — below the 1000 km/h gate,
	// consistent with an actual transpacific flight. Factor must not
	// fire.
	now := time.Date(2026, 4, 25, 12, 0, 0, 0, time.UTC)
	repo := &stubSessionRepo{
		mostRecent: &models.AuthSessionDoc{
			UserUUID:  "u1",
			IPAddress: locNYC.IP,
			CreatedAt: now.Add(-12 * time.Hour),
		},
		fpCount: 4,
		ipCount: 4,
	}
	resolver := &stubGeoResolver{locs: map[string]*geoip.Location{
		locNYC.IP:   locNYC,
		locTokyo.IP: locTokyo,
	}}
	scorer := newScorerWithGeo(t, repo, resolver, DefaultImpossibleTravelVelocityKmh, now)
	got, _ := scorer.AssessLoginRisk(context.Background(), "u1", &models.SecurityContext{
		IPAddress:   locTokyo.IP,
		Fingerprint: "fp-known",
	})
	if got.Score != 0 {
		t.Errorf("realistic flight should not trip factor: got %v (factors=%+v)", got.Score, got.Factors)
	}
}

func TestAssessLoginRisk_ImpossibleTravelIgnoresShortHop(t *testing.T) {
	// Rome → Milan is ~475 km — well above the 100 km minimum, but in
	// one hour that's ~475 km/h, still below the 1000 km/h gate. And
	// a 30-second VPN hop Rome→Milan is under 100 km? Actually Rome-
	// Milan is ~475 km so it's over the min-distance gate. Use two IPs
	// in the same metro area (distance < 100 km) to exercise the
	// min-distance gate explicitly.
	now := time.Date(2026, 4, 25, 12, 0, 0, 0, time.UTC)
	locRomeA := &geoip.Location{Country: "IT", Latitude: 41.9028, Longitude: 12.4964}
	locRomeB := &geoip.Location{Country: "IT", Latitude: 41.9500, Longitude: 12.5500} // ~7 km away
	repo := &stubSessionRepo{
		mostRecent: &models.AuthSessionDoc{
			UserUUID:  "u1",
			IPAddress: "roma-a",
			CreatedAt: now.Add(-10 * time.Second), // extreme velocity if distance were large
		},
		fpCount: 4,
		ipCount: 4,
	}
	resolver := &stubGeoResolver{locs: map[string]*geoip.Location{
		"roma-a": locRomeA,
		"roma-b": locRomeB,
	}}
	scorer := newScorerWithGeo(t, repo, resolver, DefaultImpossibleTravelVelocityKmh, now)
	got, _ := scorer.AssessLoginRisk(context.Background(), "u1", &models.SecurityContext{
		IPAddress:   "roma-b",
		Fingerprint: "fp-known",
	})
	// The rapid_ip_change factor still fires (different IP <5min ago)
	// but impossible_travel must not: distance is <100 km.
	for _, f := range got.Factors {
		if f.Details["factor"] == "impossible_travel" {
			t.Errorf("short-hop must not trip impossible_travel: %+v", f)
		}
	}
}

func TestAssessLoginRisk_ImpossibleTravelSkipsOnMissingGeoLookup(t *testing.T) {
	// Resolver returns (nil, nil) for one of the IPs. Factor must stay
	// inert without erroring.
	now := time.Date(2026, 4, 25, 12, 0, 0, 0, time.UTC)
	repo := &stubSessionRepo{
		mostRecent: &models.AuthSessionDoc{
			UserUUID:  "u1",
			IPAddress: locNYC.IP,
			CreatedAt: now.Add(-1 * time.Hour),
		},
		fpCount: 4,
		ipCount: 4,
	}
	// Only the current IP has a geo entry; prior is absent.
	resolver := &stubGeoResolver{locs: map[string]*geoip.Location{
		locTokyo.IP: locTokyo,
	}}
	scorer := newScorerWithGeo(t, repo, resolver, DefaultImpossibleTravelVelocityKmh, now)
	got, _ := scorer.AssessLoginRisk(context.Background(), "u1", &models.SecurityContext{
		IPAddress:   locTokyo.IP,
		Fingerprint: "fp-known",
	})
	for _, f := range got.Factors {
		if f.Details["factor"] == "impossible_travel" {
			t.Errorf("missing prior geo must skip factor: %+v", f)
		}
	}
}

func TestAssessLoginRisk_ImpossibleTravelSkipsOnNoopResolver(t *testing.T) {
	// Default scorer (no GeoIP) must not compute impossible_travel
	// regardless of the other factors. Safety net against a future
	// refactor that accidentally defaults the resolver.
	now := time.Date(2026, 4, 25, 12, 0, 0, 0, time.UTC)
	repo := &stubSessionRepo{
		mostRecent: &models.AuthSessionDoc{
			UserUUID:  "u1",
			IPAddress: "1.2.3.4",
			CreatedAt: now.Add(-1 * time.Minute),
		},
		fpCount: 4,
		ipCount: 0, // new IP fires
	}
	scorer := newScorer(t, repo, now) // NoopResolver
	got, _ := scorer.AssessLoginRisk(context.Background(), "u1", &models.SecurityContext{
		IPAddress:   "5.6.7.8",
		Fingerprint: "fp-known",
	})
	// new_ip + rapid_ip_change should fire (0.2 + 0.4 = 0.6). impossible_travel
	// must not.
	for _, f := range got.Factors {
		if f.Details["factor"] == "impossible_travel" {
			t.Errorf("NoopResolver must skip impossible_travel, got %+v", f)
		}
	}
}

func TestAssessLoginRisk_FactorCountErrorDoesNotAbort(t *testing.T) {
	// A mid-scoring repo error (fpErr / ipErr) should skip that factor
	// but continue evaluating the rest.
	now := time.Date(2026, 4, 25, 12, 0, 0, 0, time.UTC)
	repo := &stubSessionRepo{
		mostRecent: &models.AuthSessionDoc{
			UserUUID:  "u1",
			IPAddress: "10.0.0.5",
			CreatedAt: now.Add(-24 * time.Hour),
		},
		fpErr:   errors.New("count failed"),
		ipCount: 0, // new IP still fires
	}
	scorer := newScorer(t, repo, now)
	got, _ := scorer.AssessLoginRisk(context.Background(), "u1", &models.SecurityContext{
		IPAddress:   "8.8.8.8",
		Fingerprint: "fp-something",
	})
	if got.Score != WeightNewIP {
		t.Errorf("partial failure: want new_ip only (%v), got %v", WeightNewIP, got.Score)
	}
}
