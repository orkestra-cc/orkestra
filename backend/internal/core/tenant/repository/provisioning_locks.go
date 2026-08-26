package repository

import (
	"context"
	"errors"

	"github.com/orkestra/backend/internal/core/tenant/models"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// CollProvisioningLocks is the platform-global provisioning-cardinality
// lock collection: one row per TenantKind, holding nothing but a revision
// counter. Like CollDefaults it deliberately carries no tenantId — it
// guards how many tenants of a tier may exist, so it cannot itself be
// scoped by one.
//
// The row is not data. It exists solely so that two transactions competing
// for the same tier's provisioning slot write a SHARED document and
// therefore collide, which is the only thing that makes a count-then-write
// cardinality check safe. See RunProvisioningGuarded.
const CollProvisioningLocks = "tenant_provisioning_locks"

// ErrProvisioningSlotTaken is the repository-level cardinality refusal: the
// tier already has as many slot occupants as the caller allowed. The
// service maps it to its own ErrProvisioningLocked sentinel (which the
// handlers turn into 409 tenant.provisioning_locked); the repository does
// not import the service package.
var ErrProvisioningSlotTaken = errors.New("tenant: provisioning slot already occupied")

// RunProvisioningGuarded runs write inside a transaction that admits it
// only while at most maxSlots tenants of kind occupy a provisioning slot.
//
// **Why a transaction and a lock row rather than a plain count-then-write.**
// The `single` invariant is a COUNTING constraint, and counting cannot be
// enforced by a unique index: `single` is a runtime config an operator can
// switch on and off, so the schema must keep permitting several tenants per
// tier. The check therefore has to read before it writes — and a bare
// read-then-write is a TOCTOU gap that a transaction alone does NOT close.
// MongoDB transactions are snapshot-isolated with no read-write conflict
// detection: two concurrent creations would each count zero, each insert a
// DIFFERENT tenant document, collide on nothing, and both commit. That is
// exactly the race this guard exists to remove — the same shape as the
// first-assignment hole fixed in RunDefaultGuarded (see defaults.go).
//
// So the transaction opens by bumping the per-kind lock row, upserting it
// when absent. Both competitors now write one shared document, MongoDB
// raises a WriteConflict, and session.WithTransaction retries the loser on
// the TransientTransactionError label. The retry re-runs this whole closure
// against a fresh snapshot, so its count sees the winner's committed tenant
// and the cardinality check refuses correctly.
//
// The lock row's unique `kind` index (module.go::Collections()) is
// load-bearing: without it two from-scratch upserts each insert their own
// document with its own _id, nothing conflicts, and the guard silently
// degrades back to the unsafe count-then-write it replaces.
//
// Unlike RunDefaultGuarded's placeholder, this row is NOT deleted
// afterwards: it is a permanent per-kind lock, carries no tenant identity,
// and is never read as state by anything.
//
// Repository methods invoked from write with sc as their ctx participate in
// the same transaction automatically. Requires a replica set, like every
// withTxn caller.
func (r *Repository) RunProvisioningGuarded(ctx context.Context, kind models.TenantKind, maxSlots int64, write func(sc mongo.SessionContext) error) error {
	return r.withTxn(ctx, func(sc mongo.SessionContext) error {
		//tenantscope:allow system: platform-global provisioning lock keyed by kind (bump first so a concurrent creation or restore of the same tier conflicts and retries against this transaction's outcome)
		if _, err := r.db.Collection(CollProvisioningLocks).UpdateOne(sc,
			bson.M{"kind": string(kind)},
			bson.M{"$inc": bson.M{"revision": int64(1)}},
			options.Update().SetUpsert(true),
		); err != nil {
			return err
		}

		n, err := r.CountProvisioningSlotsByKind(sc, kind)
		if err != nil {
			return err
		}
		if n >= maxSlots {
			return ErrProvisioningSlotTaken
		}

		return write(sc)
	})
}
