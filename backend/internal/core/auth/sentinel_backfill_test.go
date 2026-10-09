package auth

// H-6, second half (spec §4.7 D31): the tier guard of D30 protects a FRESH
// install. An install upgraded from before the sentinel existed has an
// administrator and an UNCLAIMED sentinel, so the next operator-tier OAuth
// signup would still win it. Start seeds the sentinel on behalf of the
// oldest existing super_admin.

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync"
	"testing"

	"github.com/orkestra/backend/pkg/sdk/iface"
)

// backfillUsers is the operator user provider as the backfill sees it: a
// SystemRoleHolderFinder over a seeded list (oldest first) and GetUserCount
// for the legacy fallback. Nothing else on UserProvider is reached.
type backfillUsers struct {
	iface.UserProvider
	mu          sync.Mutex
	superAdmins []string
	count       int64
	findErr     error
}

func (u *backfillUsers) seedSuperAdmins(uuids ...string) {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.superAdmins = append(u.superAdmins, uuids...)
	u.count = int64(len(u.superAdmins))
}

func (u *backfillUsers) setSuperAdminCount(n int64) {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.count = n
}

func (u *backfillUsers) failFindOldest(err error) {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.findErr = err
}

func (u *backfillUsers) FindOldestUserWithRole(_ context.Context, role string) (string, bool, error) {
	u.mu.Lock()
	defer u.mu.Unlock()
	if u.findErr != nil {
		return "", false, u.findErr
	}
	if role != "super_admin" || len(u.superAdmins) == 0 {
		return "", false, nil
	}
	return u.superAdmins[0], true, nil
}

func (u *backfillUsers) GetUserCount(_ context.Context, f *iface.UserFilters) (int64, error) {
	u.mu.Lock()
	defer u.mu.Unlock()
	if f != nil && f.Role == "super_admin" {
		return u.count, nil
	}
	return 0, nil
}

// backfillClaimer models the sentinel's $setOnInsert upsert: the first
// claim inserts, every later claim is a no-op that reports false.
type backfillClaimer struct {
	mu      sync.Mutex
	holder  string
	calls   int
	inserts int
}

func (c *backfillClaimer) ClaimFirstAdmin(_ context.Context, userUUID string) (bool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.calls++
	if c.holder != "" {
		return false, nil
	}
	c.holder = userUUID
	c.inserts++
	return true, nil
}

func (c *backfillClaimer) Release(_ context.Context, userUUID string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.holder == userUUID {
		c.holder = ""
	}
	return nil
}

func (c *backfillClaimer) claimCalls() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.calls
}

func (c *backfillClaimer) insertCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.inserts
}

func (c *backfillClaimer) claimedUUID() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.holder
}

// simulateConcurrentSignupClaim is a live first signup whose upsert landed
// before the backfill's.
func (c *backfillClaimer) simulateConcurrentSignupClaim(uuid string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.holder = uuid
	c.inserts++
}

type backfillDeps struct {
	users   *backfillUsers
	claimer *backfillClaimer
}

type backfillOpt func(*AuthModule)

// withoutRoleFinder is a fork's user provider that predates the seam.
func withoutRoleFinder() backfillOpt { return func(m *AuthModule) { m.roleHolderFinder = nil } }

func newBackfillModule(t *testing.T, opts ...backfillOpt) (*AuthModule, *backfillDeps) {
	t.Helper()
	d := &backfillDeps{users: &backfillUsers{}, claimer: &backfillClaimer{}}
	m := &AuthModule{
		indexLister:       fakeIndexLister{names: bothIndexed()},
		firstAdminClaimer: d.claimer,
		roleHolderFinder:  d.users,
		operatorUsers:     d.users,
		logger:            slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	for _, o := range opts {
		o(m)
	}
	return m, d
}

func TestSentinelBackfill_ClaimsForTheOldestSuperAdmin(t *testing.T) {
	m, deps := newBackfillModule(t)
	deps.users.seedSuperAdmins("u-old" /* createdAt earliest */, "u-new")

	if err := m.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if deps.claimer.claimedUUID() != "u-old" {
		t.Fatalf("claimed %q, want the oldest holder u-old", deps.claimer.claimedUUID())
	}
}

// Every replica must pick the SAME user, or two replicas race with
// different uuids and whichever loses leaves a sentinel pointing at
// somebody else.
func TestSentinelBackfill_IsIdempotentAcrossRuns(t *testing.T) {
	m, deps := newBackfillModule(t)
	deps.users.seedSuperAdmins("u-old")

	_ = m.Start(context.Background())
	_ = m.Stop(context.Background())
	_ = m.Start(context.Background())

	if deps.claimer.insertCount() != 1 {
		t.Fatalf("%d inserts across two runs, want 1 — $setOnInsert makes the second a no-op", deps.claimer.insertCount())
	}
}

// A fork's user provider may predate the seam. The backfill still runs,
// on the interface that exists, with a placeholder that is safe by the
// sentinel's own contract: nothing reads its userUUID back, and Release
// deletes only a MATCHING uuid, so no signup rollback can ever remove it.
func TestSentinelBackfill_FallsBackToGetUserCount(t *testing.T) {
	m, deps := newBackfillModule(t, withoutRoleFinder())
	deps.users.setSuperAdminCount(3)

	if err := m.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if deps.claimer.claimedUUID() != "legacy-backfill" {
		t.Fatalf("claimed %q, want the legacy-backfill placeholder", deps.claimer.claimedUUID())
	}
}

// Edge case 18: a fresh install has zero super_admins, so nothing is
// claimed and the bootstrap paths are untouched.
func TestSentinelBackfill_FreshInstallClaimsNothing(t *testing.T) {
	m, deps := newBackfillModule(t)
	if err := m.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if deps.claimer.claimCalls() != 0 {
		t.Fatal("a fresh install must not claim")
	}
}

// A lookup or claim failure logs ERROR and Start still returns nil —
// auth is a core module, and the tier guard of D30 already closes the
// client side. The next boot retries.
func TestSentinelBackfill_ErrorsDoNotFailStart(t *testing.T) {
	m, deps := newBackfillModule(t)
	deps.users.failFindOldest(errors.New("mongo down"))

	if err := m.Start(context.Background()); err != nil {
		t.Fatalf("Start must return nil: %v", err)
	}
	if deps.claimer.claimCalls() != 0 {
		t.Fatal("nothing may be claimed on a failed lookup")
	}
}

// Edge case 19: a backfill racing a live first signup converges —
// $setOnInsert means whoever upserts first wins and the loser is a
// no-op. A signup that loses gets 'guest', which is CORRECT: a
// super_admin already existed.
func TestSentinelBackfill_RacesASignupSafely(t *testing.T) {
	m, deps := newBackfillModule(t)
	deps.users.seedSuperAdmins("u-old")
	deps.claimer.simulateConcurrentSignupClaim("u-signup")

	if err := m.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if deps.claimer.insertCount() != 1 {
		t.Fatal("exactly one insert wins")
	}
	if deps.claimer.claimedUUID() != "u-signup" {
		t.Fatalf("holder %q — the first upsert must stand", deps.claimer.claimedUUID())
	}
}
