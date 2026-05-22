package services

import (
	"context"
	"log/slog"
	"time"

	"github.com/orkestra-cc/orkestra-addon-marketing/models"
)

// ActivityEmitter centralises the auto-emission helpers. It depends on
// ActivityService (the canonical write path) so the listener emits
// flow through the same Validate + DedupKey + listener-fan
// machinery manual creates use.
//
// Phase 3 emission surface (all best-effort; failures log + swallow,
// never roll back the parent mutation):
//   - tag_added / tag_removed — from PersonUpdateListener via diff
//     against the freshly-fetched after-snapshot.
//   - imported — emitted directly by the importer pipeline when it
//     persists a fresh Person row. The pipeline is the only emitter
//     because Membership rows lack a Sources[] field and per-Person
//     scope is the correct dedup unit.
//   - merged — emitted directly by ConflictReviewService.Resolve
//     when an applied resolution closes an open review.
type ActivityEmitter struct {
	activity *ActivityService
	logger   *slog.Logger
}

// NewActivityEmitter wires the emitter against an ActivityService.
func NewActivityEmitter(svc *ActivityService, logger *slog.Logger) *ActivityEmitter {
	return &ActivityEmitter{activity: svc, logger: logger}
}

// OnPersonUpdated computes the tag-set delta between before and after
// + fires one Activity per added / removed tag.
//
// Why diff at the listener instead of at the call site: PersonService.Update
// accepts a raw bson-shaped patch and can come from many code paths
// (handler, importer auto-merge, conflict-review resolve). Diffing
// against the freshly-fetched after-snapshot is the only reliable place
// to detect the actual set delta.
func (e *ActivityEmitter) OnPersonUpdated(ctx context.Context, before, after models.Person) {
	diff := tagDiff(before.Tags, after.Tags)
	for _, tag := range diff.Added {
		e.emit(ctx, &models.Activity{
			PersonUUID: after.UUID,
			Kind:       models.KindTagAdded,
			OccurredAt: time.Now().UTC(),
			Source:     models.ActivitySourceSystem,
			ExternalID: "tag_added:" + tag + ":" + after.UUID,
			Payload:    map[string]any{"tagUuid": tag},
		})
	}
	for _, tag := range diff.Removed {
		e.emit(ctx, &models.Activity{
			PersonUUID: after.UUID,
			Kind:       models.KindTagRemoved,
			OccurredAt: time.Now().UTC(),
			Source:     models.ActivitySourceSystem,
			ExternalID: "tag_removed:" + tag + ":" + after.UUID,
			Payload:    map[string]any{"tagUuid": tag},
		})
	}
}

// EmitImported is the direct emission hook the importer pipeline calls
// when it persists (or merges into) a Person row. ExternalID anchors
// the dedupKey on (personUuid, jobUuid) so re-running the same job is
// a no-op.
func (e *ActivityEmitter) EmitImported(ctx context.Context, personUUID, orgUUID, importJobUUID string) {
	if personUUID == "" || importJobUUID == "" {
		return
	}
	e.emit(ctx, &models.Activity{
		PersonUUID: personUUID,
		OrgUUID:    orgUUID,
		Kind:       models.KindImported,
		OccurredAt: time.Now().UTC(),
		Source:     models.ActivitySourceImporter,
		ExternalID: "imported:" + importJobUUID + ":" + personUUID,
		Payload:    map[string]any{"importJobUuid": importJobUUID},
		Refs:       models.ActivityRefs{ImportJobUUID: importJobUUID},
	})
}

// EmitMerged is the direct emission hook the conflict-review resolve
// path calls when an applied resolution closes a review. The
// reviewUUID + personUUID combination is the dedup key; replays of
// the same resolve are no-ops at the dedupKey unique index.
func (e *ActivityEmitter) EmitMerged(ctx context.Context, personUUID, orgUUID, conflictReviewUUID string) {
	if personUUID == "" || conflictReviewUUID == "" {
		return
	}
	e.emit(ctx, &models.Activity{
		PersonUUID: personUUID,
		OrgUUID:    orgUUID,
		Kind:       models.KindMerged,
		OccurredAt: time.Now().UTC(),
		Source:     models.ActivitySourceSystem,
		ExternalID: "merged:" + conflictReviewUUID + ":" + personUUID,
		Payload:    map[string]any{"conflictReviewUuid": conflictReviewUUID},
	})
}

// emit submits the Activity through ActivityService.Create + logs but
// does not propagate errors.
func (e *ActivityEmitter) emit(ctx context.Context, a *models.Activity) {
	if e.activity == nil {
		return
	}
	if _, err := e.activity.Create(ctx, a); err != nil && e.logger != nil {
		e.logger.WarnContext(ctx, "marketing: auto-emission failed",
			slog.String("kind", string(a.Kind)),
			slog.String("personUuid", a.PersonUUID),
			slog.String("err", err.Error()),
		)
	}
}

// TagDiff holds the (Added, Removed) sets produced by tagDiff. Exposed
// for tests in this package.
type TagDiff struct {
	Added   []string
	Removed []string
}

func tagDiff(before, after []string) TagDiff {
	beforeSet := make(map[string]struct{}, len(before))
	for _, t := range before {
		beforeSet[t] = struct{}{}
	}
	afterSet := make(map[string]struct{}, len(after))
	for _, t := range after {
		afterSet[t] = struct{}{}
	}
	var added, removed []string
	for t := range afterSet {
		if _, ok := beforeSet[t]; !ok {
			added = append(added, t)
		}
	}
	for t := range beforeSet {
		if _, ok := afterSet[t]; !ok {
			removed = append(removed, t)
		}
	}
	return TagDiff{Added: added, Removed: removed}
}
