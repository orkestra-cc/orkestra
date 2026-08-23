package repository

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/orkestra/backend/internal/core/tenant/models"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// CollDefaults is the platform-global default-tenant pointer collection.
// It deliberately carries no tenantId: it identifies the tenant used to
// BEGIN operator resolution, so it cannot itself be scoped by an
// already-resolved tenant. See models.TenantDefault.
const CollDefaults = "tenant_defaults"

var (
	// ErrDefaultGuard aborts a lifecycle transition targeting the platform
	// default; callers map it to 409 tenant.default_reassignment_required.
	ErrDefaultGuard = errors.New("tenant: target is the platform default")
	// ErrDefaultTargetNotOperational rejects pointing the default at a
	// tenant that is not operational (active, not soft-deleted) internal.
	ErrDefaultTargetNotOperational = errors.New("tenant: default target not operational")
	// ErrDefaultAlreadyAssigned aborts AssignDefault when a pointer already
	// exists for kind and names a DIFFERENT tenant than the one requested.
	// Detected INSIDE the same transaction that would otherwise write the
	// pointer — not via a read that happens before the transaction starts —
	// so two concurrent AssignDefault calls racing to assign different
	// tenants to an unassigned platform cannot both observe "unassigned"
	// and silently overwrite one another. Whichever transaction's write to
	// the tenant_defaults singleton commits first forces the other to
	// retry (session.WithTransaction on a TransientTransactionError label,
	// same mechanism documented on RunDefaultGuarded below); the retry
	// re-reads the pointer from scratch and correctly observes the
	// winner's committed row, so the loser is guaranteed to see this error
	// rather than silently losing the race.
	ErrDefaultAlreadyAssigned = errors.New("tenant: default already assigned to a different tenant")
)

// withTxn runs fn inside a MongoDB multi-document transaction. Requires a
// replica-set (or sharded cluster) deployment — StartSession/WithTransaction
// fail outright against a standalone mongod, which does not support
// transactions at all.
func (r *Repository) withTxn(ctx context.Context, fn func(sc mongo.SessionContext) error) error {
	sess, err := r.db.Client().StartSession()
	if err != nil {
		return fmt.Errorf("tenant: start session: %w", err)
	}
	defer sess.EndSession(ctx)
	_, err = sess.WithTransaction(ctx, func(sc mongo.SessionContext) (any, error) {
		return nil, fn(sc)
	})
	return err
}

// GetDefault returns the platform default-tenant pointer row for kind.
// Returns ErrNotFound when no pointer has been assigned yet.
func (r *Repository) GetDefault(ctx context.Context, kind models.TenantKind) (*models.TenantDefault, error) {
	var d models.TenantDefault
	//tenantscope:allow system: platform-global default pointer keyed by kind (read the pointer row for kind)
	err := r.db.Collection(CollDefaults).FindOne(ctx, bson.M{"kind": string(kind)}).Decode(&d)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &d, nil
}

// validateOperationalTarget resolves tenantUUID within session sc and
// confirms it is an operational tenant of the matching kind — status
// active, deletedAt nil (see SetDefault's doc for the full predicate
// rationale). Shared by SetDefault and AssignDefault so both pointer-write
// paths enforce identically the same target predicate inside their
// transaction.
func (r *Repository) validateOperationalTarget(sc mongo.SessionContext, kind models.TenantKind, tenantUUID string) error {
	var target models.Tenant
	//tenantscope:allow system: platform-global default pointer keyed by kind (validate the target is an operational tenant of the matching kind before assignment)
	terr := r.db.Collection(CollTenants).FindOne(sc, bson.M{
		"uuid":      tenantUUID,
		"kind":      string(kind),
		"deletedAt": nil,
		"status":    string(models.TenantStatusActive),
	}).Decode(&target)
	if errors.Is(terr, mongo.ErrNoDocuments) {
		return ErrDefaultTargetNotOperational
	}
	return terr
}

// SetDefault points the platform default pointer for kind at tenantUUID.
//
// The target is validated INSIDE the same transaction that writes the
// pointer: it must be an operational tenant of the matching kind — status
// active, deletedAt nil. A Tier-2 tenant, or a suspended/archived/
// soft-deleted internal tenant, is rejected with
// ErrDefaultTargetNotOperational; note this "operational tenant" predicate
// is distinct from the provisioning-slot predicate used elsewhere in this
// module (which also admits `provisioning` and `suspended`) — the default
// pointer only ever names a tenant that is fully active right now.
//
// actorUUID may be empty for an automated migration; in that case
// updatedBy is left entirely unset on the pointer row rather than stored
// as an empty-string sentinel (see models.TenantDefault). source records
// provenance and must be one of the models.DefaultUpdateSource* constants.
//
// requireExisting=true (admin transfer) fails with ErrNotFound when no
// pointer row exists yet for kind — a transfer only makes sense once a
// default has already been assigned. SetDefault ALWAYS overwrites an
// existing pointer to point at tenantUUID regardless of what it previously
// named — that unconditional-replace behavior is exactly what a transfer
// needs. It is therefore only safe to call with requireExisting=true (the
// sanctioned admin-transfer path, TransferDefaultTenant); a caller that
// wants "assign only if unassigned or already this tenant" — the
// setup/migration entry point — MUST use AssignDefault instead, which
// performs that conflict check atomically inside its own transaction. Do
// not call SetDefault with requireExisting=false from new code: a
// requireExisting=false call cannot distinguish "no pointer yet" from "a
// DIFFERENT tenant is already the default" without a race-prone read
// before the transaction starts.
//
// Returns the previous pointer target's UUID, or "" when there was none.
func (r *Repository) SetDefault(ctx context.Context, kind models.TenantKind, tenantUUID, actorUUID, source string, requireExisting bool) (string, error) {
	var prevUUID string
	err := r.withTxn(ctx, func(sc mongo.SessionContext) error {
		if verr := r.validateOperationalTarget(sc, kind, tenantUUID); verr != nil {
			return verr
		}

		var current models.TenantDefault
		//tenantscope:allow system: platform-global default pointer keyed by kind (read the current pointer to capture the previous target and enforce requireExisting)
		cerr := r.db.Collection(CollDefaults).FindOne(sc, bson.M{"kind": string(kind)}).Decode(&current)
		switch {
		case cerr == nil:
			prevUUID = current.TenantUUID
		case errors.Is(cerr, mongo.ErrNoDocuments):
			if requireExisting {
				return ErrNotFound
			}
			prevUUID = ""
		default:
			return cerr
		}

		now := time.Now()
		setFields := bson.M{
			"tenantUUID":   tenantUUID,
			"updateSource": source,
			"updatedAt":    now,
		}
		update := bson.M{
			"$set":         setFields,
			"$inc":         bson.M{"revision": int64(1)},
			"$setOnInsert": bson.M{"kind": string(kind), "createdAt": now},
		}
		if actorUUID != "" {
			setFields["updatedBy"] = actorUUID
		} else {
			// Migration provenance rule: updatedBy must be ABSENT, never an
			// empty-string sentinel, when there is no acting UUID.
			update["$unset"] = bson.M{"updatedBy": ""}
		}

		//tenantscope:allow system: platform-global default pointer keyed by kind (upsert the singleton pointer row for kind)
		_, uerr := r.db.Collection(CollDefaults).UpdateOne(sc, bson.M{"kind": string(kind)}, update, options.Update().SetUpsert(true))
		return uerr
	})
	if err != nil {
		return "", err
	}
	return prevUUID, nil
}

// AssignDefault is the setup/migration entry point: it points the platform
// default pointer for kind at tenantUUID only when no pointer exists yet
// for kind, OR the existing pointer already names tenantUUID. Unlike
// SetDefault (which always overwrites), the "does a conflicting pointer
// already exist" check runs INSIDE the same transaction as the
// read-then-write, not via a separate read before the transaction starts —
// see ErrDefaultAlreadyAssigned for why that atomicity is load-bearing.
//
// Returns created=true when a write actually happened (a genuine first
// assignment for kind). Returns created=false with a nil error when the
// pointer already named tenantUUID — an idempotent no-op: no repository
// write is performed, so callers must not treat this as a new assignment
// (e.g. for audit purposes). Returns ErrDefaultAlreadyAssigned when a
// pointer already exists and names a DIFFERENT tenant.
//
// Target validation and the actorUUID/source/updatedBy rules are identical
// to SetDefault — see its doc.
func (r *Repository) AssignDefault(ctx context.Context, kind models.TenantKind, tenantUUID, actorUUID, source string) (bool, error) {
	var created bool
	err := r.withTxn(ctx, func(sc mongo.SessionContext) error {
		// Reset on every attempt: session.WithTransaction retries this
		// entire closure from scratch on a transient conflict, including
		// after the closure itself already returned nil once (a
		// transient failure can occur during commit, after the write
		// below already ran and set created=true for that aborted
		// attempt). Only the LAST invocation's outcome — the one that
		// actually commits — must be reflected in the return value.
		created = false

		if verr := r.validateOperationalTarget(sc, kind, tenantUUID); verr != nil {
			return verr
		}

		var current models.TenantDefault
		//tenantscope:allow system: platform-global default pointer keyed by kind (read the current pointer, inside the same transaction as the write, so an assign-time conflict is detected atomically rather than via a pre-transaction read)
		cerr := r.db.Collection(CollDefaults).FindOne(sc, bson.M{"kind": string(kind)}).Decode(&current)
		switch {
		case cerr == nil:
			if current.TenantUUID == tenantUUID {
				return nil // idempotent no-op: already assigned to this tenant
			}
			return ErrDefaultAlreadyAssigned
		case !errors.Is(cerr, mongo.ErrNoDocuments):
			return cerr
		}

		now := time.Now()
		setFields := bson.M{
			"tenantUUID":   tenantUUID,
			"updateSource": source,
			"updatedAt":    now,
		}
		update := bson.M{
			"$set":         setFields,
			"$inc":         bson.M{"revision": int64(1)},
			"$setOnInsert": bson.M{"kind": string(kind), "createdAt": now},
		}
		if actorUUID != "" {
			setFields["updatedBy"] = actorUUID
		} else {
			// Migration provenance rule: updatedBy must be ABSENT, never an
			// empty-string sentinel, when there is no acting UUID.
			update["$unset"] = bson.M{"updatedBy": ""}
		}

		//tenantscope:allow system: platform-global default pointer keyed by kind (insert the singleton pointer row for kind on first assignment; the preceding read of the same document, inside this transaction, is what makes the conflict check atomic)
		_, uerr := r.db.Collection(CollDefaults).UpdateOne(sc, bson.M{"kind": string(kind)}, update, options.Update().SetUpsert(true))
		if uerr != nil {
			return uerr
		}
		created = true
		return nil
	})
	if err != nil {
		return false, err
	}
	return created, nil
}

// RunDefaultGuarded runs write inside a transaction that first bumps the
// pointer Revision for kind (a no-op — matching zero documents — when no
// pointer exists yet) and aborts with ErrDefaultGuard, without invoking
// write, when the pointer currently names targetUUID.
//
// Because SetDefault and this method both write the same tenant_defaults
// singleton as their first mutation, MongoDB's write-conflict retry
// (session.WithTransaction retries on a TransientTransactionError label)
// serializes a concurrent default transfer against a lifecycle mutation
// (suspend, archive, soft-delete) of either tenant involved: whichever
// transaction's write to the pointer commits first forces the other to
// retry — and re-validate — against the now-committed state.
//
// Repository methods invoked from write with sc as their ctx participate
// in the same transaction automatically.
func (r *Repository) RunDefaultGuarded(ctx context.Context, kind models.TenantKind, targetUUID string, write func(sc mongo.SessionContext) error) error {
	return r.withTxn(ctx, func(sc mongo.SessionContext) error {
		//tenantscope:allow system: platform-global default pointer keyed by kind (bump the pointer revision first so a concurrent transfer's write conflicts and retries against this transaction's outcome)
		if _, err := r.db.Collection(CollDefaults).UpdateOne(sc, bson.M{"kind": string(kind)}, bson.M{"$inc": bson.M{"revision": int64(1)}}); err != nil {
			return err
		}

		var current models.TenantDefault
		//tenantscope:allow system: platform-global default pointer keyed by kind (read the pointer after the revision bump to check whether it guards targetUUID)
		err := r.db.Collection(CollDefaults).FindOne(sc, bson.M{"kind": string(kind)}).Decode(&current)
		if err != nil && !errors.Is(err, mongo.ErrNoDocuments) {
			return err
		}
		if err == nil && current.TenantUUID == targetUUID {
			return ErrDefaultGuard
		}

		return write(sc)
	})
}
