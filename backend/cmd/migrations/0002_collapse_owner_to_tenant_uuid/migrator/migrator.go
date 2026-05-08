// Package migrator holds the per-collection rename logic for the Unified
// Client Aggregate Phase 4 one-shot. Decoupled from the Mongo wiring so the
// transformation can be exercised end-to-end against in-memory fakes.
//
// The migrator iterates the six collections that previously carried a
// polymorphic owner pair and rewrites each row's bson fields in place:
//
//   - $set tenantUUID := ownerUUID   (the post-Phase-3 ownerUUID always
//     points at a Tenant — clientbilling's user-owned rows were pivoted
//     to ownerKind="tenant" + ownerUUID=tenantUUID)
//   - $unset ownerKind, ownerUUID    (no longer needed; the Go structs
//     dropped these fields in the same release)
//
// Idempotency is enforced via a single sentinel in migrations_applied.
// The migrator stamps the sentinel only after every collection completes
// without error; a partial run replays cleanly because $rename on a row
// that already has tenantUUID and no ownerUUID is a no-op (no documents
// match the filter).
package migrator

import (
	"context"
	"fmt"
	"log/slog"
	"time"
)

// MigrationName is the sentinel key used in migrations_applied.
const MigrationName = "0002_collapse_owner_to_tenant_uuid"

// Collections is the canonical list of owner-scoped collections this
// migration rewrites. Exported so the operator runbook can grep for the
// exact set without spelunking through code.
var Collections = []string{
	"subscriptions_subscriptions",
	"subscriptions_invoices",
	"subscriptions_activity",
	"payments_transactions",
	"payments_payment_methods",
	"tenant_entitlements",
}

// CollectionResult reports the outcome of one collection rewrite.
type CollectionResult struct {
	Collection string
	Matched    int64
	Modified   int64
}

// Summary aggregates the run.
type Summary struct {
	Skipped    bool // sentinel already present
	Results    []CollectionResult
	DurationMS int64
}

// Store is the cross-collection seam the migrator uses. The Mongo
// implementation lives next to main.go; tests provide in-memory fakes.
type Store interface {
	// SentinelExists reports whether the sentinel for this migration is
	// already present in migrations_applied.
	SentinelExists(ctx context.Context) (bool, error)
	// RenameOwnerToTenantUUID applies the bson rewrite on a single
	// collection: every doc with `ownerUUID` set is updated to copy that
	// value into `tenantUUID` and drop both `ownerKind` and `ownerUUID`.
	// Returns the matched and modified counts.
	RenameOwnerToTenantUUID(ctx context.Context, collection string) (matched, modified int64, err error)
	// MarkSentinel stamps the migration as complete with the per-collection
	// counts so the audit row carries the same numbers the operator sees
	// in the log line.
	MarkSentinel(ctx context.Context, results []CollectionResult) error
}

// Migrator orchestrates the rename. Wire DryRun=true to make the rename
// calls log-only — the store still reports counts so the operator can
// preview.
type Migrator struct {
	Store  Store
	Logger *slog.Logger
	DryRun bool
}

// Run iterates Collections and rewrites each. Errors short-circuit so
// the sentinel is only stamped after every collection succeeds — a
// partial run leaves the sentinel absent and the next invocation
// resumes from the same starting point (the rename filter naturally
// skips already-renamed rows).
func (m *Migrator) Run(ctx context.Context) (Summary, error) {
	if m.Logger == nil {
		m.Logger = slog.Default()
	}
	start := time.Now()
	var sum Summary

	done, err := m.Store.SentinelExists(ctx)
	if err != nil {
		return sum, fmt.Errorf("sentinel lookup: %w", err)
	}
	if done {
		sum.Skipped = true
		sum.DurationMS = time.Since(start).Milliseconds()
		m.Logger.Info("migration already applied, skipping",
			slog.String("migration", MigrationName))
		return sum, nil
	}

	sum.Results = make([]CollectionResult, 0, len(Collections))
	for _, coll := range Collections {
		res := CollectionResult{Collection: coll}
		if m.DryRun {
			m.Logger.Info("would rename ownerUUID → tenantUUID",
				slog.String("collection", coll))
			sum.Results = append(sum.Results, res)
			continue
		}
		matched, modified, err := m.Store.RenameOwnerToTenantUUID(ctx, coll)
		if err != nil {
			sum.DurationMS = time.Since(start).Milliseconds()
			return sum, fmt.Errorf("rename %s: %w", coll, err)
		}
		res.Matched = matched
		res.Modified = modified
		sum.Results = append(sum.Results, res)
		m.Logger.Info("collection rewritten",
			slog.String("collection", coll),
			slog.Int64("matched", matched),
			slog.Int64("modified", modified))
	}

	if !m.DryRun {
		if err := m.Store.MarkSentinel(ctx, sum.Results); err != nil {
			sum.DurationMS = time.Since(start).Milliseconds()
			return sum, fmt.Errorf("mark sentinel: %w", err)
		}
	}
	sum.DurationMS = time.Since(start).Milliseconds()
	return sum, nil
}
