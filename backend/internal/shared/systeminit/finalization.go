package systeminit

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// keySetupFinalization is the fixed document key for the setup-finalization
// saga record. Separate from keyFirstAdmin: the two documents have
// deliberately independent lifecycles (see package doc / firstadmin.go) —
// first_admin is a rollback-capable claim on the first super_admin seat,
// while setup_finalization is the persistent record of the 4-stage
// config -> tenant -> default -> finish saga plus the boot-reconciliation
// lease. Nothing here ever deletes the setup_finalization document.
const keySetupFinalization = "setup_finalization"

// Setup-finalization sources: how the record came to exist.
const (
	SourceFresh          = "fresh"
	SourceLegacyRecovery = "legacy_recovery"
	SourceMigration      = "migration"
)

// Saga stages. Record.Stage holds the NEXT stage to execute (1..4);
// StageDone (5) plus CompletedAt marks the saga finished.
const (
	StageConfig  = 1 // provisioning.internal.mode persisted
	StageTenant  = 2 // reserved internal tenant ensured + reconciled
	StageDefault = 3 // platform default assigned
	StageFinish  = 4 // phase flips to complete
	StageDone    = 5
)

// FinalizationResult is the terminal snapshot written once the saga
// completes (Stage == StageDone).
type FinalizationResult struct {
	TenantUUID string `bson:"tenantUUID" json:"tenantUUID"`
	TenantName string `bson:"tenantName" json:"tenantName"`
	TenantSlug string `bson:"tenantSlug" json:"tenantSlug"`
	Mode       string `bson:"mode" json:"mode"`
}

// FinalizationRecord is the setup_finalization document.
type FinalizationRecord struct {
	Key                   string               `bson:"key"`
	AdminUUID             string               `bson:"adminUUID,omitempty"`
	Source                string               `bson:"source"`
	TenantUUID            string               `bson:"tenantUUID,omitempty"` // pre-minted reserved UUID
	TenantName            string               `bson:"tenantName,omitempty"` // normalized
	TenantSlug            string               `bson:"tenantSlug,omitempty"` // normalized
	Mode                  string               `bson:"mode,omitempty"`       // "manual" | "single"
	RequestHash           string               `bson:"requestHash,omitempty"`
	Stage                 int                  `bson:"stage"`
	Revision              int64                `bson:"revision"`
	StageCompletedAt      map[string]time.Time `bson:"stageCompletedAt,omitempty"` // keys "1".."4"
	LeaseOwner            string               `bson:"leaseOwner,omitempty"`
	LeaseUntil            *time.Time           `bson:"leaseUntil,omitempty"`
	CreatedAt             time.Time            `bson:"createdAt"`
	UpdatedAt             time.Time            `bson:"updatedAt"`
	CompletedAt           *time.Time           `bson:"completedAt,omitempty"`
	ReconciliationVersion int                  `bson:"reconciliationVersion"`
	ReconcileLeaseOwner   string               `bson:"reconcileLeaseOwner,omitempty"`
	ReconcileLeaseUntil   *time.Time           `bson:"reconcileLeaseUntil,omitempty"`
	Result                *FinalizationResult  `bson:"result,omitempty"`
}

// FinalizationStore is the narrow contract shared/setup and the tenant
// module consume. *Repo implements it; neither consumer opens system_init
// directly. Every mutator is a CAS: `ok=false` means the filter did not
// match (somebody else won) — reread with Get and re-evaluate.
type FinalizationStore interface {
	Get(ctx context.Context) (*FinalizationRecord, error) // (nil, nil) when absent
	// InitializeFresh upserts the record after successful initial-admin
	// creation ($setOnInsert only — never clobbers an existing record).
	InitializeFresh(ctx context.Context, adminUUID string) error
	// EnsureRecord creates the record during boot reconciliation when it is
	// missing: source=migration + completed result, or source=legacy_recovery
	// with empty adminUUID and stage=StageConfig. $setOnInsert only.
	EnsureRecord(ctx context.Context, source string, completed *FinalizationResult) (*FinalizationRecord, error)
	// ReserveRequest fills the reservation fields iff no request is reserved
	// yet (requestHash absent) and adminUUID matches the caller.
	ReserveRequest(ctx context.Context, adminUUID, tenantUUID, name, slug, mode, requestHash string) (ok bool, err error)
	ClaimStage(ctx context.Context, requestHash string, stage int, revision int64, owner string, leaseUntil time.Time) (ok bool, err error)
	RenewLease(ctx context.Context, owner string, leaseUntil time.Time) (ok bool, err error)
	AdvanceStage(ctx context.Context, owner string, stage int, revision int64) (ok bool, err error)
	// Complete is AdvanceStage for StageFinish: also writes Result + CompletedAt.
	Complete(ctx context.Context, owner string, revision int64, result FinalizationResult) (ok bool, err error)
	// ClaimRecovery atomically replaces the bound admin using the previously
	// observed UUID + revision as the CAS filter (spec: finalizer access).
	ClaimRecovery(ctx context.Context, observedAdminUUID string, revision int64, newAdminUUID string) (ok bool, err error)
	ClaimReconcileLease(ctx context.Context, version int, owner string, leaseUntil time.Time) (ok bool, err error)
	FinishReconcile(ctx context.Context, version int, owner string) (ok bool, err error)
}

// Get reads the setup_finalization record. (nil, nil) means the record does
// not exist yet — not an error, since a fresh install boots with none.
func (r *Repo) Get(ctx context.Context) (*FinalizationRecord, error) {
	var rec FinalizationRecord
	//tenantscope:allow system: platform-global setup coordinator keyed by fixed key (phase read)
	err := r.coll.FindOne(ctx, bson.M{"key": keySetupFinalization}).Decode(&rec)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("systeminit: read setup finalization: %w", err)
	}
	return &rec, nil
}

// InitializeFresh upserts the setup_finalization record after successful
// initial-admin creation. $setOnInsert only — an existing record (e.g. a
// concurrent caller who got there first, or a record recovered by boot
// reconciliation) is never clobbered.
func (r *Repo) InitializeFresh(ctx context.Context, adminUUID string) error {
	if adminUUID == "" {
		return errors.New("systeminit: adminUUID is required")
	}
	now := time.Now().UTC()
	//tenantscope:allow system: platform-global setup coordinator keyed by fixed key (fresh-install init upsert)
	_, err := r.coll.UpdateOne(ctx,
		bson.M{"key": keySetupFinalization},
		bson.M{"$setOnInsert": bson.M{
			"key": keySetupFinalization, "adminUUID": adminUUID, "source": SourceFresh,
			"stage": StageConfig, "revision": int64(1), "reconciliationVersion": 0,
			"createdAt": now, "updatedAt": now,
		}},
		options.Update().SetUpsert(true),
	)
	if err != nil {
		return fmt.Errorf("systeminit: initialize setup finalization: %w", err)
	}
	return nil
}

// EnsureRecord creates the setup_finalization record during boot
// reconciliation when it is missing. $setOnInsert only — if the record
// already exists this is a no-op and the existing record is returned.
//
// completed != nil produces a source=migration record that is already
// marked complete (an upgrade from a pre-finalization install that already
// has a working tenant). completed == nil produces a source=legacy_recovery
// record with an empty adminUUID sitting at StageConfig, for an install
// where finalization needs to run from scratch.
func (r *Repo) EnsureRecord(ctx context.Context, source string, completed *FinalizationResult) (*FinalizationRecord, error) {
	now := time.Now().UTC()
	setOnInsert := bson.M{
		"key": keySetupFinalization, "source": source,
		"revision": int64(1), "reconciliationVersion": 0,
		"createdAt": now, "updatedAt": now,
	}
	if completed != nil {
		setOnInsert["stage"] = StageDone
		setOnInsert["completedAt"] = now
		setOnInsert["result"] = completed
	} else {
		setOnInsert["stage"] = StageConfig
	}
	//tenantscope:allow system: platform-global setup coordinator keyed by fixed key (boot-reconciliation ensure upsert)
	_, err := r.coll.UpdateOne(ctx,
		bson.M{"key": keySetupFinalization},
		bson.M{"$setOnInsert": setOnInsert},
		options.Update().SetUpsert(true),
	)
	if err != nil {
		return nil, fmt.Errorf("systeminit: ensure setup finalization record: %w", err)
	}
	return r.Get(ctx)
}

// ReserveRequest fills the reservation fields iff no request is reserved yet
// (requestHash absent or empty) and the record's adminUUID matches the
// caller. Two concurrent callers racing to reserve produce exactly one
// winner; the loser rereads via Get and sees the winner's hash.
func (r *Repo) ReserveRequest(ctx context.Context, adminUUID, tenantUUID, name, slug, mode, requestHash string) (bool, error) {
	now := time.Now().UTC()
	//tenantscope:allow system: platform-global setup coordinator keyed by fixed key (request reservation CAS)
	res, err := r.coll.UpdateOne(ctx,
		bson.M{
			"key": keySetupFinalization, "adminUUID": adminUUID,
			"requestHash": bson.M{"$in": bson.A{nil, ""}},
			"completedAt": nil,
		},
		bson.M{
			"$set": bson.M{
				"tenantUUID": tenantUUID, "tenantName": name, "tenantSlug": slug,
				"mode": mode, "requestHash": requestHash,
				"stage": StageConfig, "updatedAt": now,
			},
			"$inc": bson.M{"revision": int64(1)},
		},
	)
	if err != nil {
		return false, fmt.Errorf("systeminit: reserve finalization request: %w", err)
	}
	return res.ModifiedCount > 0, nil
}

// ClaimStage acquires the stage lease for (requestHash, stage, revision) iff
// no lease is currently live (leaseUntil absent or already expired). A false
// return means someone else holds the lease or the (stage, revision) has
// already moved on — not an error.
func (r *Repo) ClaimStage(ctx context.Context, requestHash string, stage int, revision int64, owner string, leaseUntil time.Time) (bool, error) {
	now := time.Now().UTC()
	//tenantscope:allow system: platform-global setup coordinator keyed by fixed key (stage lease CAS)
	res, err := r.coll.UpdateOne(ctx,
		bson.M{
			"key": keySetupFinalization, "requestHash": requestHash,
			"stage": stage, "revision": revision,
			"$or": bson.A{
				bson.M{"leaseUntil": nil},
				bson.M{"leaseUntil": bson.M{"$lt": now}},
			},
		},
		bson.M{"$set": bson.M{"leaseOwner": owner, "leaseUntil": leaseUntil, "updatedAt": now}},
	)
	if err != nil {
		return false, fmt.Errorf("systeminit: claim stage lease: %w", err)
	}
	return res.ModifiedCount > 0, nil
}

// RenewLease extends the current lease owner's leaseUntil without touching
// stage or revision. A false return means owner is no longer the lease
// holder (lost the lease, or the stage already advanced past it).
func (r *Repo) RenewLease(ctx context.Context, owner string, leaseUntil time.Time) (bool, error) {
	now := time.Now().UTC()
	//tenantscope:allow system: platform-global setup coordinator keyed by fixed key (lease renewal CAS)
	res, err := r.coll.UpdateOne(ctx,
		bson.M{"key": keySetupFinalization, "leaseOwner": owner},
		bson.M{"$set": bson.M{"leaseUntil": leaseUntil, "updatedAt": now}},
	)
	if err != nil {
		return false, fmt.Errorf("systeminit: renew stage lease: %w", err)
	}
	return res.ModifiedCount > 0, nil
}

// AdvanceStage completes the current stage held by owner: bumps Stage by
// one, increments Revision, releases the lease, and stamps
// StageCompletedAt[stage]. A false return means owner no longer holds the
// lease at that (stage, revision) — a stale owner cannot advance.
func (r *Repo) AdvanceStage(ctx context.Context, owner string, stage int, revision int64) (bool, error) {
	now := time.Now().UTC()
	//tenantscope:allow system: platform-global setup coordinator keyed by fixed key (stage advance CAS)
	res, err := r.coll.UpdateOne(ctx,
		bson.M{"key": keySetupFinalization, "leaseOwner": owner, "stage": stage, "revision": revision},
		bson.M{
			"$set": bson.M{
				"stage": stage + 1, "updatedAt": now,
				"stageCompletedAt." + strconv.Itoa(stage): now,
			},
			"$unset": bson.M{"leaseOwner": "", "leaseUntil": ""},
			"$inc":   bson.M{"revision": int64(1)},
		},
	)
	if err != nil {
		return false, fmt.Errorf("systeminit: advance stage: %w", err)
	}
	return res.ModifiedCount > 0, nil
}

// Complete is AdvanceStage specialized for StageFinish: it also writes the
// terminal Result snapshot and CompletedAt, and lands Stage on StageDone
// rather than StageFinish+1 (they are numerically equal, but StageDone is
// the named terminal state).
func (r *Repo) Complete(ctx context.Context, owner string, revision int64, result FinalizationResult) (bool, error) {
	now := time.Now().UTC()
	//tenantscope:allow system: platform-global setup coordinator keyed by fixed key (saga completion CAS)
	res, err := r.coll.UpdateOne(ctx,
		bson.M{"key": keySetupFinalization, "leaseOwner": owner, "stage": StageFinish, "revision": revision},
		bson.M{
			"$set": bson.M{
				"stage": StageDone, "updatedAt": now,
				"stageCompletedAt." + strconv.Itoa(StageFinish): now,
				"completedAt": now, "result": result,
			},
			"$unset": bson.M{"leaseOwner": "", "leaseUntil": ""},
			"$inc":   bson.M{"revision": int64(1)},
		},
	)
	if err != nil {
		return false, fmt.Errorf("systeminit: complete setup finalization: %w", err)
	}
	return res.ModifiedCount > 0, nil
}

// ClaimRecovery atomically replaces the bound admin using the previously
// observed UUID + revision as the CAS filter. An empty observedAdminUUID
// matches a record whose adminUUID is absent or empty (the legacy_recovery
// case). Two concurrent recovery claims observing the same (adminUUID,
// revision) produce exactly one winner.
func (r *Repo) ClaimRecovery(ctx context.Context, observedAdminUUID string, revision int64, newAdminUUID string) (bool, error) {
	if newAdminUUID == "" {
		return false, errors.New("systeminit: newAdminUUID is required")
	}
	now := time.Now().UTC()
	filter := bson.M{"key": keySetupFinalization, "revision": revision}
	if observedAdminUUID == "" {
		filter["adminUUID"] = bson.M{"$in": bson.A{nil, ""}}
	} else {
		filter["adminUUID"] = observedAdminUUID
	}
	//tenantscope:allow system: platform-global setup coordinator keyed by fixed key (finalizer-access recovery CAS)
	res, err := r.coll.UpdateOne(ctx,
		filter,
		bson.M{
			"$set": bson.M{"adminUUID": newAdminUUID, "updatedAt": now},
			"$inc": bson.M{"revision": int64(1)},
		},
	)
	if err != nil {
		return false, fmt.Errorf("systeminit: claim finalization recovery: %w", err)
	}
	return res.ModifiedCount > 0, nil
}

// ClaimReconcileLease acquires the boot-reconciliation lease for the given
// target version iff the record's current reconciliationVersion is behind
// that target AND no reconcile lease is currently live. This lease is
// strictly independent from the stage lease above — a different mechanism
// electing a single replica to run upgrade-time reconciliation work.
//
// The consumer contract guarantees a record exists (via EnsureRecord)
// before this is ever called, so this is a plain CAS on an existing
// document — no upsert semantics needed here.
func (r *Repo) ClaimReconcileLease(ctx context.Context, version int, owner string, leaseUntil time.Time) (bool, error) {
	now := time.Now().UTC()
	//tenantscope:allow system: platform-global setup coordinator keyed by fixed key (reconcile lease CAS)
	res, err := r.coll.UpdateOne(ctx,
		bson.M{
			"key":                   keySetupFinalization,
			"reconciliationVersion": bson.M{"$lt": version},
			"$or": bson.A{
				bson.M{"reconcileLeaseUntil": nil},
				bson.M{"reconcileLeaseUntil": bson.M{"$lt": now}},
			},
		},
		bson.M{"$set": bson.M{"reconcileLeaseOwner": owner, "reconcileLeaseUntil": leaseUntil, "updatedAt": now}},
	)
	if err != nil {
		return false, fmt.Errorf("systeminit: claim reconcile lease: %w", err)
	}
	return res.ModifiedCount > 0, nil
}

// FinishReconcile releases the reconcile lease held by owner and advances
// reconciliationVersion to version. A false return means owner no longer
// holds the reconcile lease (lost it, or it was already finished by a
// concurrent caller — reconciliation is idempotent so this is safe to
// ignore).
func (r *Repo) FinishReconcile(ctx context.Context, version int, owner string) (bool, error) {
	now := time.Now().UTC()
	//tenantscope:allow system: platform-global setup coordinator keyed by fixed key (reconcile lease release CAS)
	res, err := r.coll.UpdateOne(ctx,
		bson.M{"key": keySetupFinalization, "reconcileLeaseOwner": owner},
		bson.M{
			"$set":   bson.M{"reconciliationVersion": version, "updatedAt": now},
			"$unset": bson.M{"reconcileLeaseOwner": "", "reconcileLeaseUntil": ""},
		},
	)
	if err != nil {
		return false, fmt.Errorf("systeminit: finish reconcile: %w", err)
	}
	return res.ModifiedCount > 0, nil
}

var _ FinalizationStore = (*Repo)(nil)
