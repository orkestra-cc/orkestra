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
// default has already been assigned. Use requireExisting=false for the
// first assignment (setup) or a migration backfill.
//
// Returns the previous pointer target's UUID, or "" when there was none.
func (r *Repository) SetDefault(ctx context.Context, kind models.TenantKind, tenantUUID, actorUUID, source string, requireExisting bool) (string, error) {
	var prevUUID string
	err := r.withTxn(ctx, func(sc mongo.SessionContext) error {
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
		if terr != nil {
			return terr
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
