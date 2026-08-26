package repository

// Mongo-gated tests for the interaction between the (tenantId, userUUID,
// roleId) unique index and binding EXPIRY. They reuse
// binding_ensure_integration_test.go's harness (liveBindingRepository,
// ensureRaceBinding), including its MONGO_TEST_URI gate and the unique
// index it builds exactly as module.go's Collections() declares it.
//
// The invariant under test: an EXPIRED binding confers nothing —
// ListActiveBindingsForUser filters it out — so it must never stand in the
// way of granting the same role again. Before the unique index shipped,
// that was true for free: a re-grant simply inserted a second row. With the
// constraint in place, an expired row occupies the tuple permanently
// (authz_bindings has no TTL and no reaper — see this module's CLAUDE.md),
// which turned "expired" into "un-re-grantable": CreateBinding surfaced
// E11000 (the service maps it to a 409) and EnsureBinding returned the dead
// row while reporting success, so the OwnerRoleBinder call sites
// (CreateTenant, SetMemberRoles, AttachMember) granted nothing at all.

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"go.mongodb.org/mongo-driver/bson"
)

// TestEnsureBinding_ExpiredRowDoesNotBlockRegrant is the core regression:
// a tuple occupied by an expired grant must come back LIVE after an ensure,
// and the user must actually hold the role afterwards.
//
// Before the fix EnsureBinding's $setOnInsert upsert found the expired row,
// matched, inserted nothing, and returned it verbatim — so the caller saw a
// successful "grant" while ListActiveBindingsForUser still reported zero
// active bindings. That is the silent-no-op path OwnerRoleBinder rides on.
func TestEnsureBinding_ExpiredRowDoesNotBlockRegrant(t *testing.T) {
	repo, cleanup := liveBindingRepository(t)
	defer cleanup()
	ctx := context.Background()

	past := time.Now().Add(-1 * time.Hour)
	stale := ensureRaceBinding(uuid.NewString(), "user-expired", "tenant-A", "role-org-owner", "system")
	stale.ExpiresAt = &past
	if err := repo.CreateBinding(ctx, stale); err != nil {
		t.Fatalf("seed expired binding: %v", err)
	}

	// Sanity: the seeded grant confers nothing.
	active, err := repo.ListActiveBindingsForUser(ctx, "user-expired", "tenant-A")
	if err != nil {
		t.Fatalf("ListActiveBindingsForUser (seed): %v", err)
	}
	if len(active) != 0 {
		t.Fatalf("seeded expired binding still reports as active: %d row(s)", len(active))
	}

	fresh := ensureRaceBinding(uuid.NewString(), "user-expired", "tenant-A", "role-org-owner", "system")
	out, err := repo.EnsureBinding(ctx, fresh)
	if err != nil {
		t.Fatalf("EnsureBinding over an expired row: %v", err)
	}
	if out.ExpiresAt != nil {
		t.Fatalf("EnsureBinding returned a still-expiring row (expiresAt=%v): the expired grant was reused instead of replaced", out.ExpiresAt)
	}

	active, err = repo.ListActiveBindingsForUser(ctx, "user-expired", "tenant-A")
	if err != nil {
		t.Fatalf("ListActiveBindingsForUser (after ensure): %v", err)
	}
	if len(active) != 1 {
		t.Fatalf("after EnsureBinding the user holds %d active binding(s), want 1 — the grant was a silent no-op", len(active))
	}

	// Still exactly one row for the tuple: the expired one was replaced,
	// not accumulated alongside.
	//tenantscope:allow test assertion counts rows in one isolated per-run test database
	count, err := repo.db.Collection(CollBindings).CountDocuments(ctx, bson.M{
		"tenantId": "tenant-A", "userUUID": "user-expired", "roleId": "role-org-owner",
	})
	if err != nil {
		t.Fatalf("CountDocuments: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected exactly one persisted binding for the tuple, got %d", count)
	}
}

// TestCreateBinding_ExpiredRowDoesNotBlockRegrant covers the non-ensure
// entry point: the plain admin grant (POST .../authz/bindings). An expired
// incumbent used to make it fail with a duplicate-key error, which the
// service maps to ErrBindingExists → 409 "already granted" — about a grant
// that confers nothing and that the operator has no way to clear from the
// UI other than deleting the invisible expired binding first.
func TestCreateBinding_ExpiredRowDoesNotBlockRegrant(t *testing.T) {
	repo, cleanup := liveBindingRepository(t)
	defer cleanup()
	ctx := context.Background()

	past := time.Now().Add(-1 * time.Hour)
	stale := ensureRaceBinding(uuid.NewString(), "user-recreate", "tenant-A", "role-org-admin", "system")
	stale.ExpiresAt = &past
	if err := repo.CreateBinding(ctx, stale); err != nil {
		t.Fatalf("seed expired binding: %v", err)
	}

	fresh := ensureRaceBinding(uuid.NewString(), "user-recreate", "tenant-A", "role-org-admin", "admin-uuid")
	if err := repo.CreateBinding(ctx, fresh); err != nil {
		t.Fatalf("CreateBinding over an expired row: %v (an expired grant must not read as a live duplicate)", err)
	}

	active, err := repo.ListActiveBindingsForUser(ctx, "user-recreate", "tenant-A")
	if err != nil {
		t.Fatalf("ListActiveBindingsForUser: %v", err)
	}
	if len(active) != 1 {
		t.Fatalf("after CreateBinding the user holds %d active binding(s), want 1", len(active))
	}
	if active[0].UUID != fresh.UUID {
		t.Fatalf("active binding is %q, want the freshly granted %q", active[0].UUID, fresh.UUID)
	}
}

// TestCreateBinding_LiveRowStillConflicts is the other half of the
// contract: only a GENUINELY expired incumbent is displaced. A live grant —
// permanent, or expiring in the future — must still produce the
// duplicate-key error the service maps to ErrBindingExists, otherwise the
// unique index would silently stop meaning anything.
func TestCreateBinding_LiveRowStillConflicts(t *testing.T) {
	repo, cleanup := liveBindingRepository(t)
	defer cleanup()
	ctx := context.Background()

	cases := []struct {
		name    string
		expires *time.Time
	}{
		{"permanent", nil},
		{"expires in the future", func() *time.Time { v := time.Now().Add(24 * time.Hour); return &v }()},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			user := "user-live-" + uuid.NewString()
			incumbent := ensureRaceBinding(uuid.NewString(), user, "tenant-A", "role-org-owner", "system")
			incumbent.ExpiresAt = tc.expires
			if err := repo.CreateBinding(ctx, incumbent); err != nil {
				t.Fatalf("seed live binding: %v", err)
			}

			dup := ensureRaceBinding(uuid.NewString(), user, "tenant-A", "role-org-owner", "someone-else")
			if err := repo.CreateBinding(ctx, dup); err == nil {
				t.Fatal("CreateBinding over a LIVE row succeeded: the unique constraint no longer surfaces as a duplicate")
			}
		})
	}
}

// TestEnsureBinding_LiveRowIsNeverDisplaced pins the same boundary for the
// ensure path, and specifically guards the BSON comparison trap the expiry
// filter has to avoid: MongoDB's canonical type ordering sorts null BELOW
// dates, so a bare {"expiresAt": {"$lte": now}} predicate also matches
// permanent grants (expiresAt null or missing) and would reap them. A
// permanent incumbent must survive an ensure completely untouched.
func TestEnsureBinding_LiveRowIsNeverDisplaced(t *testing.T) {
	repo, cleanup := liveBindingRepository(t)
	defer cleanup()
	ctx := context.Background()

	future := time.Now().Add(24 * time.Hour)
	cases := []struct {
		name    string
		expires *time.Time
	}{
		{"permanent", nil},
		{"expires in the future", &future},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			user := "user-keep-" + uuid.NewString()
			incumbent := ensureRaceBinding(uuid.NewString(), user, "tenant-A", "role-org-owner", "system")
			incumbent.ExpiresAt = tc.expires
			out1, err := repo.EnsureBinding(ctx, incumbent)
			if err != nil {
				t.Fatalf("EnsureBinding (seed): %v", err)
			}

			replay := ensureRaceBinding(uuid.NewString(), user, "tenant-A", "role-org-owner", "someone-else")
			out2, err := repo.EnsureBinding(ctx, replay)
			if err != nil {
				t.Fatalf("EnsureBinding (replay): %v", err)
			}
			if out2.UUID != out1.UUID {
				t.Fatalf("replay displaced a live binding: UUID %q, want the incumbent's %q", out2.UUID, out1.UUID)
			}
			if out2.GrantedBy != "system" {
				t.Fatalf("replay overwrote GrantedBy on a live binding: %q", out2.GrantedBy)
			}
			switch {
			case tc.expires == nil && out2.ExpiresAt != nil:
				t.Fatalf("replay stamped an expiry onto a permanent grant: %v", out2.ExpiresAt)
			case tc.expires != nil && out2.ExpiresAt == nil:
				t.Fatal("replay cleared the incumbent's expiry")
			}
		})
	}
}
