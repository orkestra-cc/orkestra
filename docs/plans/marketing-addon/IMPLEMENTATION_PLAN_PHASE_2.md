---
type: implementation-plan
author: claude
date: 2026-05-21
domain: orkestra
component: marketing-addon
status: ready-to-execute
phase: 2 (Storicizzazione & scoring)
branch: feature/marketing-phase-2
---

# Marketing Addon — Phase 2 Implementation Plan

Concrete plan to land **Phase 2 (Storicizzazione & scoring)** from
[`Orkestra_marketing_addon.md`](Orkestra_marketing_addon.md) §9 on
top of the Phase 1 scaffold that shipped to `dev` on 2026-05-19
(`52ca4b6`).

Phase 1 reference plan: [`IMPLEMENTATION_PLAN.md`](IMPLEMENTATION_PLAN.md).
Phases 3–5 are explicitly **out of scope** and will be planned
separately once Phase 2 is in `dev`.

Decisions resolved with the user before this plan was written
(2026-05-21):

- **PR split**: 5 sequential sub-PRs on `feature/marketing-phase-2`
  (one for data layer, one for the pure score engine, one for the
  eager + nightly scheduler, one for auto-emission + API surface,
  one for frontend + docs).
- **Frontend scope**: **complete** — timeline tab on contact detail,
  `/marketing/scoring` admin (profile CRUD + leaderboard +
  breakdown drawer), manual activity entry form.
- **Activity `kind` registry**: **static enum** in
  `models/activity_kinds.go`. Adding a new kind = patch to this
  file. No runtime `RegisterKind(...)` API; defer that until a
  concrete second-addon consumer asks for it.
- **Plan language**: English, parity with Phase 1.

## Phase 2 deliverables (from §9)

What ships at the end of this plan:

- 3 new MongoDB collections: `marketing_activities`,
  `marketing_score_profiles`, `marketing_score_snapshots`.
- Append-only enforcement on `marketing_activities` at the service
  layer (no `UpdateOne`/`DeleteOne` repository surface except the
  GDPR-only `HardDeleteByPersonUUID` flow).
- Pure score engine
  (`scoring.Compute(activities, profile, asOf) → ScoreResult`) — a
  side-effect-free function of `[]Activity` + `ScoreProfile` + `asOf`
  with full per-activity breakdown.
- Eager incremental recompute on every activity insert + nightly
  full recompute via `Startable` (24h ticker, immediate tick at boot).
- Cedar permissions: `marketing.activity.read`,
  `marketing.activity.write`, `marketing.score_profile.write`.
  Config: `score.recompute_interval`, `score.eager_on_insert`,
  `activity.breakdown_max_entries`.
- Huma API surface (operator audience only):
  - `GET /v1/marketing/persons/{uuid}/activities` (timeline)
  - `POST /v1/marketing/activities` (manual: `call_made`,
    `meeting_held`, `note_added`, `corrected_by`)
  - `GET /v1/marketing/score-profiles` + CRUD
  - `GET /v1/marketing/persons/{uuid}/scores` (every profile for a
    person)
  - `GET /v1/marketing/score-profiles/{uuid}/leaderboard` (top N
    snapshots by value desc)
  - `GET /v1/marketing/score-snapshots/{uuid}` (single, with
    breakdown)
- Auto-emission of `imported` / `merged` / `tag_added` /
  `tag_removed` from the existing Phase-1 write paths.
- Admin UI:
  - new timeline tab on `/marketing/contacts/:id` (URL-synced via
    `?tab=timeline`)
  - new score sub-tab on the same page listing the person's
    snapshots with a breakdown drawer
  - new `/marketing/scoring` page: profile list + CRUD form + per-
    profile leaderboard
  - new "Add activity" affordance on contact detail (call / meeting
    / note)
- One example profile (`hot_lead`) seeded for the enterprise SKU on
  first boot (so the engine has something to compute against in
  smoke tests).

Explicitly **deferred** to Phase 3+:

- The `corrected_by` UI flow (Phase 3 review queue).
- Score snapshots history collection (`marketing_score_snapshots_history`).
- `agents`-addon integration for AI-assisted scoring (Phase 5).
- Card-issued / card-status-changed activity kinds (Phase 4 fills
  the payloads; their enum entries land now since they're cheap and
  read-only on the engine side).

---

## 1. Branch + workflow

```bash
git checkout dev
git pull
git checkout -b feature/marketing-phase-2
```

Sub-PR strategy onto `feature/marketing-phase-2` (each independently
green on CI; merge to branch, not yet to `dev`):

1. **PR-1**: data layer — 3 new collections (`marketing_activities`,
   `marketing_score_profiles`, `marketing_score_snapshots`), models,
   repositories with `tenantrepo`, indexes via `Collections()`,
   append-only service-level gate. Repo tests.
2. **PR-2**: pure score engine — `scoring/` package with
   `Compute(...)`, decay functions (`none`/`linear`/`exponential`),
   rule matching (kind, payload, window, cap). Pure-function unit
   tests with table-driven cases.
3. **PR-3**: eager + nightly recompute — `ScoreService` orchestrates
   compute → upsert snapshot; `RecomputeJob` ticker via
   `Startable`. Profile-version `stale` invalidation on profile
   write. Concurrency: per-tenant serialization to avoid races
   between eager and nightly on the same `(person, profile)` key.
4. **PR-4**: API + auto-emission — activity handler (timeline +
   manual create), score-profile CRUD handler, snapshot read
   handler, leaderboard handler. Cedar permissions wired. Auto-
   emission of `imported` / `merged` / `tag_added` / `tag_removed`
   from existing Phase-1 services.
5. **PR-5**: frontend (timeline tab, scoring admin, leaderboard,
   breakdown drawer, manual-activity form) + RTK Query slice
   extension + docs (CLAUDE.md updates, OpenAPI dump regen).

Each PR opens against `feature/marketing-phase-2`; the final squash
to `dev` happens after PR-5.

---

## 2. Module additions (PR-1 wiring)

### 2.1 Collection constants

Extend `models/collections.go` with the 3 new names:

```go
const (
    // existing...
    ActivitiesCollection     = "marketing_activities"
    ScoreProfilesCollection  = "marketing_score_profiles"
    ScoreSnapshotsCollection = "marketing_score_snapshots"
)
```

### 2.2 Collections() index declarations

Add to `MarketingModule.Collections()` in `module.go`:

| Collection | Indexes (verbatim from schemas/*.md) |
|---|---|
| `marketing_activities` | `(tenantId, personUuid, occurredAt desc)`; `(tenantId, orgUuid, occurredAt desc)` sparse; `(tenantId, kind, occurredAt desc)`; `dedupKey` unique; `(tenantId, refs.campaignUuid)` sparse; `(tenantId, refs.eventUuid)` sparse; `(tenantId, refs.cardUuid)` sparse; `(tenantId, source, recordedAt desc)`; plus the standard `uuid` unique |
| `marketing_score_profiles` | `(tenantId, name)` unique; `(tenantId, active)`; plus `uuid` unique |
| `marketing_score_snapshots` | `(tenantId, personUuid, profileUuid)` unique; `(tenantId, profileUuid, value desc)`; `(tenantId, profileUuid, stale)`; `(tenantId, personUuid)`; plus `uuid` unique |

> **Index naming note.** The schemas reference snake_case fields
> (`person_id`, `occurred_at`); Phase 1 normalized to camelCase BSON
> tags (`personUuid`, `occurredAt`). This plan follows the Phase-1
> precedent — schemas remain the design artifact, Go is the source of
> truth for on-disk field names. Same approach used by `marketing_persons`
> (`emails.address` vs the schema's `primary_email`).

### 2.3 Permissions

Append to `Permissions()` in `module.go`:

```go
{Key: "marketing.activity.read",       Module: m.Name(), Description: "View the activity log of a contact"},
{Key: "marketing.activity.write",      Module: m.Name(), Description: "Create manual activities (call, meeting, note) and correction events"},
{Key: "marketing.score_profile.write", Module: m.Name(), Description: "Create and update scoring profiles (rules, decay, filters)"},
```

A separate `marketing.score_profile.read` is **not** declared —
read access rides on `marketing.contact.read` (any operator who can
see contacts can see the scoring that ranks them; granting view
of a leaderboard without contact-read access would be incoherent).

### 2.4 ConfigSchema

Append to `ConfigSchema()`:

```go
{Key: "scoreRecomputeInterval",  Label: "Score nightly-recompute interval", Type: module.FieldDuration, Default: "24h",     EnvVar: "MARKETING_SCORE_RECOMPUTE_INTERVAL"},
{Key: "scoreEagerOnInsert",      Label: "Recompute score on activity insert", Type: module.FieldBool,    Default: "true",    EnvVar: "MARKETING_SCORE_EAGER_ON_INSERT"},
{Key: "activityBreakdownMax",    Label: "Max breakdown entries per snapshot", Type: module.FieldInt,     Default: "100",     EnvVar: "MARKETING_ACTIVITY_BREAKDOWN_MAX"},
```

`scoreRecomputeInterval` is a `time.Duration`, not a cron — matches
the `subscriptions` / `billing` ticker pattern. Cron-style "at 3am"
is a Phase 2.5 swap (replace ticker with `robfig/cron/v3`) if a
real need appears.

### 2.5 NavItems

Add one entry to the existing nav block:

```go
{Name: "Scoring", Icon: "chart-line", Path: "/marketing/scoring", Active: true},
```

Timeline is **not** a top-level nav item — it lives inside the
existing contact detail page as a URL-synced tab.

### 2.6 Init() wiring

`Init()` grows by ~25 lines:

```go
actRepo     := repository.NewActivityRepository(deps.DB)
profileRepo := repository.NewScoreProfileRepository(deps.DB)
snapRepo    := repository.NewScoreSnapshotRepository(deps.DB)

actSvc      := services.NewActivityService(actRepo, mshipRepo)            // mship dependency: deriving orgUuid at write time
engine      := scoring.NewEngine(deps.Logger)                              // pure, no deps
scoreSvc    := services.NewScoreService(snapRepo, profileRepo, actRepo, engine, deps.Logger)
profileSvc  := services.NewScoreProfileService(profileRepo, scoreSvc)      // profileSvc.Save() invalidates snapshots via scoreSvc

actSvc.RegisterListener(scoreSvc.OnActivityInserted)                       // eager recompute hook

// Auto-emission hooks into Phase-1 services
personSvc.RegisterListener(autoemit.PersonListener(actSvc))
tagSvc.RegisterListener(autoemit.TagListener(actSvc))
importSvc.RegisterListener(autoemit.ImportListener(actSvc))

m.activityHandler  = handlers.NewActivityHandler(actSvc, scoreSvc)
m.profileHandler   = handlers.NewScoreProfileHandler(profileSvc)
m.snapshotHandler  = handlers.NewSnapshotHandler(scoreSvc)
m.recomputeJob     = jobs.NewRecomputeJob(scoreSvc, deps.Config.GetDuration("scoreRecomputeInterval"), deps.Logger)
```

The `RegisterListener` pattern keeps Phase-1 services free of a
`*ActivityService` dependency — listeners are typed callback
functions, registered in `Init()` after both producers and consumers
exist. Decoupled enough to support `agents`-addon scoring listeners
in Phase 5 without touching Phase-1/2 services.

### 2.7 Start() / Stop()

Add to `module.go`:

```go
func (m *MarketingModule) Start(ctx context.Context) error {
    go m.recomputeJob.Start(ctx)
    return nil
}

func (m *MarketingModule) Stop(ctx context.Context) error {
    m.recomputeJob.Stop()
    return nil
}
```

The job pattern matches
`backend/internal/addons/subscriptions/jobs/renewal_job.go` —
goroutine + ticker + `stopChan`, immediate tick on start so a fresh
boot catches anything already stale.

---

## 3. Data layer (PR-1)

### 3.1 Activity model

`models/activity.go`:

```go
type Activity struct {
    ID          primitive.ObjectID `bson:"_id,omitempty"`
    UUID        string             `bson:"uuid"`
    TenantUUID  string             `bson:"tenantId"`
    PersonUUID  string             `bson:"personUuid"`
    OrgUUID     string             `bson:"orgUuid,omitempty"`
    Kind        ActivityKind       `bson:"kind"`
    OccurredAt  time.Time          `bson:"occurredAt"`
    RecordedAt  time.Time          `bson:"recordedAt"`
    Source      ActivitySource     `bson:"source"`
    Payload     map[string]any     `bson:"payload,omitempty"`
    Refs        ActivityRefs       `bson:"refs,omitempty"`
    DedupKey    string             `bson:"dedupKey"`
    ExternalID  string             `bson:"externalId,omitempty"`
    CreatedBy   string             `bson:"createdBy,omitempty"`
}

type ActivityRefs struct {
    CampaignUUID         string `bson:"campaignUuid,omitempty"`
    EventUUID            string `bson:"eventUuid,omitempty"`
    FormUUID             string `bson:"formUuid,omitempty"`
    ContentUUID          string `bson:"contentUuid,omitempty"`
    ImportJobUUID        string `bson:"importJobUuid,omitempty"`
    CardUUID             string `bson:"cardUuid,omitempty"`
    CorrectsActivityUUID string `bson:"correctsActivityUuid,omitempty"`
}
```

### 3.2 Activity kind enum

`models/activity_kinds.go` — static enum, per the resolved decision:

```go
type ActivityKind string

const (
    // Email
    KindEmailSent        ActivityKind = "email_sent"
    KindEmailOpened      ActivityKind = "email_opened"
    KindEmailClicked     ActivityKind = "email_clicked"
    KindEmailBounced     ActivityKind = "email_bounced"
    KindEmailUnsubscribed ActivityKind = "email_unsubscribed"
    KindEmailComplained  ActivityKind = "email_complained"
    // Events
    KindEventInvited     ActivityKind = "event_invited"
    KindEventRegistered  ActivityKind = "event_registered"
    KindEventAttended    ActivityKind = "event_attended"
    KindEventNoShow      ActivityKind = "event_no_show"
    KindEventCancelled   ActivityKind = "event_cancelled"
    // Web / form
    KindFormSubmitted    ActivityKind = "form_submitted"
    KindPageVisited      ActivityKind = "page_visited"
    KindContentDownloaded ActivityKind = "content_downloaded"
    // Direct
    KindCallMade         ActivityKind = "call_made"
    KindMeetingHeld      ActivityKind = "meeting_held"
    KindNoteAdded        ActivityKind = "note_added"
    // System
    KindImported         ActivityKind = "imported"
    KindMerged           ActivityKind = "merged"
    KindTagAdded         ActivityKind = "tag_added"
    KindTagRemoved       ActivityKind = "tag_removed"
    KindCardIssued       ActivityKind = "card_issued"           // Phase 4 fills payload
    KindCardStatusChanged ActivityKind = "card_status_changed"  // Phase 4 fills payload
    KindCorrectedBy      ActivityKind = "corrected_by"
)

// AllKinds is consumed by handler validation + scoring engine
// wildcard expansion.
var AllKinds = []ActivityKind{...}

// ManualKinds restricts the manual POST API surface to the 4 kinds
// an operator can legitimately type by hand.
var ManualKinds = []ActivityKind{
    KindCallMade, KindMeetingHeld, KindNoteAdded, KindCorrectedBy,
}
```

### 3.3 Activity repository — append-only enforcement

`repository/activity_repo.go` exposes:

- `Create(ctx, activity) error` — sole write path. Stamps
  `tenantId` via `tenantrepo.StampInsert`, computes `dedupKey`,
  derives `orgUuid` from the person's primary+active membership at
  write time, sets `recordedAt = now`.
- `List(ctx, filter) ([]Activity, error)` — timeline reads, ordered
  by `occurredAt desc`.
- `ListSincePerson(ctx, personUUID, since) ([]Activity, error)` —
  used by the eager recomputer to skip activities older than the
  snapshot's `lastActivityAt`.
- `ListAllForPerson(ctx, personUUID) ([]Activity, error)` — full
  history for a from-scratch recompute.
- `HardDeleteByPersonUUID(ctx, personUUID) error` — GDPR-only
  cascade. Behind `marketing.contact.delete` + the Phase-1 person
  delete flow.

**No** `Update*` / `DeleteOne` methods. Corrections happen via
`ActivityService.Correct(originalUUID, reason)` which inserts a new
activity of `kind=corrected_by` with
`refs.correctsActivityUuid=originalUUID`. The scoring engine
de-applies the corrected activity when it encounters the
correction (see §4.4).

The `dedupKey` is computed in the service layer
(`sha256(personUUID|kind|occurredAt.UTC().RFC3339|externalId)`); a
unique index on `dedupKey` makes re-imports idempotent at the DB
level — `Create` swallows the resulting `E11000` and treats it as a
no-op success (returns the existing UUID).

### 3.4 ScoreProfile + ScoreSnapshot models

`models/score_profile.go`:

```go
type ScoreProfile struct {
    ID           primitive.ObjectID `bson:"_id,omitempty"`
    UUID         string             `bson:"uuid"`
    TenantUUID   string             `bson:"tenantId"`
    Name         string             `bson:"name"`            // slug-like, unique per tenant
    Description  string             `bson:"description,omitempty"`
    Active       bool               `bson:"active"`
    Rules        []ScoreRule        `bson:"rules"`
    Filters      *ProfileFilter     `bson:"filters,omitempty"`
    DefaultDecay *DecayFn           `bson:"defaultDecay,omitempty"`
    Version      int                `bson:"version"`         // ++ on every save
    CreatedAt    time.Time          `bson:"createdAt"`
    UpdatedAt    time.Time          `bson:"updatedAt"`
    CreatedBy    string             `bson:"createdBy,omitempty"`
    UpdatedBy    string             `bson:"updatedBy,omitempty"`
}

type ScoreRule struct {
    ActivityKind any            `bson:"activityKind"`         // ActivityKind | []ActivityKind | "*"
    MatchPayload map[string]any `bson:"matchPayload,omitempty"`
    Points       float64        `bson:"points"`
    Decay        *DecayFn       `bson:"decay,omitempty"`
    Cap          *float64       `bson:"cap,omitempty"`
    WindowDays   *int           `bson:"windowDays,omitempty"`
}

type DecayFn struct {
    Fn            string `bson:"fn"`           // "none" | "linear" | "exponential"
    WindowDays    *int   `bson:"windowDays,omitempty"`
    HalfLifeDays  *int   `bson:"halfLifeDays,omitempty"`
}

type ProfileFilter struct {
    TagsInclude        []string       `bson:"tagsInclude,omitempty"`
    TagsExclude        []string       `bson:"tagsExclude,omitempty"`
    CustomFieldFilters map[string]any `bson:"customFieldFilters,omitempty"`
}
```

`models/score_snapshot.go`:

```go
type ScoreSnapshot struct {
    ID              primitive.ObjectID `bson:"_id,omitempty"`
    UUID            string             `bson:"uuid"`
    TenantUUID      string             `bson:"tenantId"`
    PersonUUID      string             `bson:"personUuid"`
    ProfileUUID     string             `bson:"profileUuid"`
    ProfileVersion  int                `bson:"profileVersion"`
    Value           float64            `bson:"value"`
    Breakdown       []BreakdownEntry   `bson:"breakdown,omitempty"`
    AsOf            time.Time          `bson:"asOf"`
    ComputedAt      time.Time          `bson:"computedAt"`
    Applicable      bool               `bson:"applicable"`
    Stale           bool               `bson:"stale"`
    ActivityCount   int                `bson:"activityCount"`
    LastActivityAt  *time.Time         `bson:"lastActivityAt,omitempty"`
}

type BreakdownEntry struct {
    ActivityUUID      string       `bson:"activityUuid"`
    ActivityKind      ActivityKind `bson:"activityKind"`
    OccurredAt        time.Time    `bson:"occurredAt"`
    RuleIndex         int          `bson:"ruleIndex"`
    RawPoints         float64      `bson:"rawPoints"`
    AppliedDecay      float64      `bson:"appliedDecay"`
    PointsContributed float64      `bson:"pointsContributed"`
}
```

Snapshot upserts go through `repository/score_snapshot_repo.go::Upsert(ctx, snap)`,
keyed on `(tenantId, personUuid, profileUuid)` (matches the unique
index). The unique key guarantees that concurrent eager + nightly
recomputes on the same `(person, profile)` settle on a single row
deterministically (latest `computedAt` wins; the snapshot is a
cache, not a primary source).

### 3.5 Tenant scoping

Same rules as Phase 1 — every query through
`tenantrepo.Scope`/`StampInsert`. The `tenantscope` analyzer can't
see this module (separate Go module, blind spot documented in
`addons/marketing/CLAUDE.md`), so reviewers must check manually
during PR-1.

### 3.6 Repository tests

One `*_test.go` per repo using the inline Mongo testkit pattern
already in `repository/normalize_test.go` (the marketing module
doesn't import `backend/internal/testkit` per its CLAUDE.md). Key
cases:

- Activity: dedupKey unique violation returns a typed sentinel.
- Activity: `HardDeleteByPersonUUID` cascade.
- ScoreProfile: version increment on `Update`.
- ScoreSnapshot: upsert on the unique key; `MarkStaleByProfile`
  flips every snapshot for the profile.

---

## 4. Score engine (PR-2)

### 4.1 Package layout

```
backend/internal/addons/marketing/scoring/
├── engine.go        # public Compute(activities, profile, asOf) -> ScoreResult
├── decay.go         # none / linear / exponential
├── match.go         # ruleMatches(activity, rule) bool — kind, payload, window
├── correction.go    # de-apply `corrected_by` chains
└── *_test.go        # table-driven, no Mongo, no I/O
```

### 4.2 Compute contract

```go
type ScoreResult struct {
    Value          float64
    Breakdown      []models.BreakdownEntry
    ActivityCount  int
    LastActivityAt *time.Time
}

func (e *Engine) Compute(
    activities []models.Activity,
    profile    *models.ScoreProfile,
    asOf       time.Time,
) ScoreResult
```

**Purity invariants:**

- No DB calls. No `time.Now()` reads — `asOf` is the only clock.
- Input `activities` is read-only.
- Output `Breakdown` ordered by `OccurredAt asc` for stable diffs.

This makes the engine trivially testable and gives time-travel for
free (`GET /v1/marketing/persons/{uuid}/scores?asOf=…` would just
pass `asOf` instead of `time.Now()`).

### 4.3 Decay functions

```go
// none:        f(age) = 1
// linear:      f(age) = max(0, 1 - age/window)
// exponential: f(age) = 2 ** (-age/halfLife)
```

`age = asOf - activity.OccurredAt`. Each is a pure function with a
unit test per branch.

### 4.4 Correction handling

Pre-pass over `activities`:

1. Build `correctedSet := { activity.RefsCorrectsActivityUUID for a in activities if a.Kind == KindCorrectedBy }`.
2. When iterating activities to compute, skip any activity whose
   UUID is in `correctedSet`. The `corrected_by` activity itself
   contributes 0 points (it's audit metadata, not an event).

This keeps the engine append-only-friendly: corrections never
mutate prior rows, they're just additional rows the engine reads.

### 4.5 Rule matching

`ruleMatches(activity, rule)` returns true when:

- `rule.ActivityKind` is `"*"`, OR equals the activity's kind, OR
  is a slice containing the kind.
- `rule.MatchPayload` (if set) — every key/value in the rule's
  payload filter passes against `activity.Payload`. Supports `$in`,
  `$eq`, `$exists`, `$ne` (lifted from Mongo-style filters; same
  surface used by `ProfileFilter.CustomFieldFilters`). A tiny pure
  matcher in `match.go` — no Mongo client, no aggregation.
- `rule.WindowDays` (if set) — `asOf - activity.OccurredAt <=
  windowDays * 24h`.

On match: `rawPoints = rule.Points`, `appliedDecay = decay.Apply(age,
rule.Decay or profile.DefaultDecay)`, `contribution = rawPoints *
appliedDecay`. After iterating every activity, apply
`rule.Cap` per-rule (sum of contributions of the rule capped at
`cap`).

### 4.6 Breakdown size cap

If breakdown exceeds `activityBreakdownMax` (default 100), drop the
lowest-contribution entries past the cap and replace them with one
synthetic aggregate entry
(`ActivityUUID="", ActivityKind="aggregate", PointsContributed=Σ(dropped)`).
Keeps document size bounded under high-volume tenants. Tested
explicitly.

### 4.7 Tests

Table-driven, exhaustive:

- One row per decay function × age bucket.
- Per-rule cap behavior (5 rules + cap = scheduled contribution).
- Wildcard kind + payload matcher.
- Correction chain (activity → corrected_by → corrected_by chain).
- `applicable=false` path (filter mismatch → engine returns
  `Value: 0, Applicable: false`).

Coverage target ≥85% on `scoring/` (engine has no I/O — high
coverage is cheap).

---

## 5. Eager + nightly scheduler (PR-3)

### 5.1 ScoreService

`services/score_service.go`:

```go
type ScoreService struct {
    snapRepo  *repository.ScoreSnapshotRepository
    profRepo  *repository.ScoreProfileRepository
    actRepo   *repository.ActivityRepository
    engine    *scoring.Engine
    logger    *slog.Logger

    perTenantMu sync.Map  // tenantUUID → *sync.Mutex (eager/nightly serialization)
}
```

Key methods:

- `OnActivityInserted(ctx, activity)` — eager hook. Looks up every
  active profile for the tenant; for each, calls
  `recomputeOne(ctx, personUUID, profile)`. Behind a per-`(tenant,
  person)` mutex to prevent two concurrent activity inserts on the
  same person from racing on the snapshot upsert.
- `recomputeOne(ctx, personUUID, profile)` — loads all activities
  for the person, applies `profile.Filters` (tags/custom fields) on
  the person itself to set `applicable`, runs `engine.Compute`,
  upserts the snapshot.
- `RecomputeStaleBatch(ctx, limit)` — used by the nightly job.
  Pulls `stale=true` snapshots in batches of `limit` (default 200),
  recomputes each, marks `stale=false` on success. Logs per-batch
  totals.
- `InvalidateProfile(ctx, profileUUID)` — called by
  `ScoreProfileService.Save` on every write. Marks every snapshot
  for the profile as `stale=true` (single `UpdateMany`, no
  recompute on the write thread). Nightly job + the next
  per-person eager event will pick them up.

### 5.2 RecomputeJob

`jobs/recompute_job.go` — clone of subscriptions'
`renewal_job.go` with `interval` defaulting to 24h and a tick body
that calls `scoreService.RecomputeStaleBatch(ctx, 200)` in a loop
until the batch returns 0:

```go
func (j *RecomputeJob) tick(ctx context.Context) {
    var total int
    for {
        n := j.score.RecomputeStaleBatch(ctx, 200)
        if n == 0 { break }
        total += n
        if ctx.Err() != nil { return }
    }
    if total > 0 {
        j.logger.Info("score recompute tick", slog.Int("recomputed", total))
    }
}
```

Stop semantics identical to `RenewalJob` — `stopChan`, `running`
guard, ctx-aware exit.

### 5.3 Eager hop-off when disabled

If `scoreEagerOnInsert=false`, `ActivityService.Create` skips
`OnActivityInserted`. The nightly job still runs (operators can
turn off eager for write-heavy import bursts and rely on nightly).
Configurable per-tenant via the existing `ConfigService` plumbing.

### 5.4 Stale invariant

A snapshot is `stale` when one of:

- Its `profileVersion` is less than the current profile version
  (profile edited since last compute).
- The profile was saved between its `computedAt` and now (covered
  by the version bump above — single signal).
- (Future Phase 3) the person's tags/customFields changed in a way
  that affects `Applicable` — not enforced in Phase 2; the next
  eager event on any kind catches it.

### 5.5 Tests

- Eager: insert activity → snapshot's `value` reflects the new
  activity's contribution.
- Nightly: mark 500 snapshots stale → batch processes them all in
  ≤3 batch rounds.
- Profile edit: increment version → every snapshot for that
  profile is `stale=true` before the next read.
- Concurrent insert: 10 goroutines insert 10 activities each for
  the same person → final snapshot value equals the deterministic
  re-compute of all 100 activities (no lost updates).

---

## 6. API + auto-emission (PR-4)

### 6.1 Endpoints

| Path | Methods | Permission | Notes |
|---|---|---|---|
| `/v1/marketing/persons/{uuid}/activities` | GET | `marketing.activity.read` (folds into `contact.read` — see §2.3) | Timeline, paged, `kind=` filter, `since=` ISO time |
| `/v1/marketing/activities` | POST | `marketing.activity.write` | Manual create — kind restricted to `ManualKinds` |
| `/v1/marketing/activities/{uuid}/correct` | POST | `marketing.activity.write` | Body: `{ reason: string }` → inserts `corrected_by` row |
| `/v1/marketing/score-profiles` | GET, POST | `contact.read` / `marketing.score_profile.write` | List + create |
| `/v1/marketing/score-profiles/{uuid}` | GET, PATCH, DELETE | `contact.read` / `score_profile.write` / `score_profile.write` | |
| `/v1/marketing/score-profiles/{uuid}/leaderboard` | GET | `contact.read` | Top N by `value desc`, paged, default 50 |
| `/v1/marketing/persons/{uuid}/scores` | GET | `contact.read` | Every snapshot for the person across all active profiles |
| `/v1/marketing/score-snapshots/{uuid}` | GET | `contact.read` | Single snapshot with full breakdown |

Activity timeline supports `kind=`, `kind[]=`, `source=`, `since=`,
`until=`, `limit`/`cursor`. Pagination via opaque cursor on
`occurredAt`.

Three new permission buckets in `RegisterRoutes`:

```go
gated.Group(func(r chi.Router) {
    r.Use(ri.Operator.AuthMW.RequirePermission("marketing.activity.read"))
    api := humachi.New(r, ri.APIConfig)
    handlers.RegisterActivityReadRoutes(api, m.activityHandler)
})

gated.Group(func(r chi.Router) {
    r.Use(ri.Operator.AuthMW.RequirePermission("marketing.activity.write"))
    api := humachi.New(r, ri.APIConfig)
    handlers.RegisterActivityWriteRoutes(api, m.activityHandler)
})

gated.Group(func(r chi.Router) {
    r.Use(ri.Operator.AuthMW.RequirePermission("marketing.score_profile.write"))
    api := humachi.New(r, ri.APIConfig)
    handlers.RegisterScoreProfileWriteRoutes(api, m.profileHandler)
})
```

Score-profile reads, leaderboard, and snapshot reads all live in
the existing `marketing.contact.read` bucket — they're query
shapes on the same operator audience that lists contacts.

### 6.2 Cedar policy coverage

The `policycoverage` analyzer is blind to this module
(extracted-addon limitation documented in
`addons/marketing/CLAUDE.md`), so no new file in
`backend/internal/core/authz/cedar/policies/` is strictly required.
Phase 2 still adds `policies/marketing.cedar` (creating it if Phase 1
did not — checked at PR-4 start) with `permit` rules for the new
keys against the operator role catalog. Same shape as
`subscriptions.cedar`. Manual `make policycoverage` is still clean
because the analyzer doesn't see this module either way; the
policy file lands so the eventual `go.work`-aware analyzer (deferred
infra) finds coverage when it ships.

### 6.3 Auto-emission

`services/autoemit/` (new package) with three listener factories:

- `PersonListener(act *ActivityService)` — returns
  `OnPersonCreated`, `OnPersonMerged` callbacks. Hooked into
  `PersonService` via a new `RegisterListener` method (additive,
  doesn't break existing call sites — the listener slice starts
  empty).
- `TagListener(act *ActivityService)` — emits `tag_added` /
  `tag_removed` when `PersonService.AddTag` / `RemoveTag` is
  called.
- `ImportListener(act *ActivityService)` — emits one `imported`
  activity per person/org touched by an import. Hooked into
  `ImportService`'s commit phase; `recordedAt=now`,
  `occurredAt=now` (we don't know the original engagement time
  from a Phase-1 CSV — that's Phase 3's engagement-export feature).

Activity emission happens **synchronously inside the same
transaction-equivalent flow** as the source mutation, but the
listener slice catches `panic`s and logs them with `slog.ErrorContext`
without rolling back the source mutation. Same trade-off
subscriptions makes for activity-log writes vs. core state.

### 6.4 Handler tests

- ActivityHandler: GET timeline returns activities ordered desc;
  filter by kind works; pagination cursor round-trips; POST manual
  rejects non-`ManualKinds`; POST correct inserts the right shape.
- ScoreProfileHandler: PATCH increments `version`, marks snapshots
  stale; GET leaderboard returns sorted by value desc.
- SnapshotHandler: GET single returns full breakdown.

---

## 7. Frontend (PR-5)

### 7.1 Module manifest extension

Extend `frontend-admin/src/modules/marketing.tsx` with the new
route:

```tsx
const ScoringPage = lazy(() => import('pages/marketing/scoring'));
// ...
{ path: 'marketing/scoring', element: wrap(<ScoringPage />, 'marketing-scoring') },
```

Contact detail already exists; the new tabs hang off its
`?tab=` param (already URL-synced from Phase 1 per the `url-tabs`
skill).

### 7.2 Pages

| Page | Purpose |
|---|---|
| `contacts/detail` (extended) | Add `?tab=timeline` (activity feed, infinite-scroll, kind filter chips) and `?tab=scores` (snapshot list with breakdown drawer); "Add activity" button opens a modal for `call_made` / `meeting_held` / `note_added` |
| `scoring` | Two-pane: left list of profiles with active/inactive toggle + version badge; right pane shows the selected profile's form (name, description, rules table with add/remove/edit, decay defaults, filters) + leaderboard preview (top 20 persons by current value) |
| `scoring/new` | Reuses the right-pane form in a dedicated route for deep-linking |

Timeline rendering uses the existing `TimelineCard` falcon
component if present; otherwise a Tailwind list with kind icon
chips matched to the kind enum.

### 7.3 RTK Query slice extension

`frontend-admin/src/store/api/marketingApi.ts` (existing) — append
endpoints:

```ts
getPersonActivities: builder.query<...>(...)
createActivity: builder.mutation<...>(...)
correctActivity: builder.mutation<...>(...)
listScoreProfiles: builder.query<...>(...)
createScoreProfile: builder.mutation<...>(...)
updateScoreProfile: builder.mutation<...>(...)
deleteScoreProfile: builder.mutation<...>(...)
getProfileLeaderboard: builder.query<...>(...)
getPersonScores: builder.query<...>(...)
getScoreSnapshot: builder.query<...>(...)
```

Cache tags: `Activity` (per person), `ScoreProfile`, `ScoreSnapshot`
(per person + per profile). Profile mutations invalidate
`ScoreSnapshot` broadly so the leaderboard refreshes when rules
change.

### 7.4 URL-sync tabs

The contact detail page tab list grows from
`overview | memberships | sources` to
`overview | memberships | timeline | scores | sources`. Every tab
switch updates `?tab=…` — already the pattern from Phase 1, no new
hook needed.

### 7.5 i18n

EN + IT strings for the new pages live in
`frontend-admin/src/locales/{en,it}.json` under a
`pages.marketing.scoring.*` and `pages.marketing.timeline.*`
namespace. Italian strings follow the conventions called out in
`MEMORY.md::project_frontend_admin_i18n_phase4` — keep loanwords
(score, tag, leaderboard) as IT-native uses them; translate
operational terms (`Recompute` → `Ricalcola`, `Stale` → `Da
ricalcolare`).

---

## 8. Docs + CI hygiene (PR-5)

### 8.1 OpenAPI dump

After PR-4 lands (routes added), regenerate:

```bash
cd backend
(cd ../docker && docker compose -f docker-compose.infra.yml up -d)
make openapi-dump
git add openapi/enterprise.json
```

`make openapi-check` will fail PR-4 / PR-5 otherwise.

### 8.2 CLAUDE.md updates

- **`backend/internal/addons/marketing/CLAUDE.md`** — flip Phase 2
  from "future" to "shipped" in the lifecycle section; add the new
  collections + routes + permissions to the per-collection table;
  add a "Scoring engine" subsection explaining the purity contract
  + eager/nightly cadence; bump the route count and permission
  count.
- **Project root `CLAUDE.md`** — the marketing row already exists;
  no edit needed. (Phase 2 doesn't add a new module, it grows the
  existing one.)
- **`backend/CLAUDE.md`** — no edit (still 14 optional addons).

### 8.3 Memory updates

Add a new memory file
`/home/tore/.claude/projects/-home-tore-orkestra/memory/project_marketing_addon_phase2.md`
with the phase-by-phase status + follow-ups (Phase 3 conflict
review queue, Phase 4 card kinds, Phase 5 ESP webhooks). Link from
`MEMORY.md` under "Active work" until the merge, then move to
"Released" after `dev → main` promotion.

### 8.4 CI gates checklist

Before merging `feature/marketing-phase-2 → dev`:

- [ ] `make ci-backend` green — covers lint, tenantscope (manual
  review on the extracted module), tests, openapi-check,
  enterprise build.
- [ ] `make ci-frontend-admin` green.
- [ ] Profile matrix builds all 6 SKUs.
- [ ] `make openapi-dump` produces no diff vs
  `openapi/enterprise.json`.
- [ ] Manual smoke: enterprise stack boots, `/marketing/scoring`
  renders, create a profile, create 3 activities (manual + import
  + tag), see snapshot value change in the timeline, leaderboard
  shows the test person.

---

## 9. Test plan

Per the test pyramid pattern from Phase 1:

**Pure engine** (`scoring/*_test.go`):

- Decay branches: none / linear / exponential, age buckets at 0,
  half-life, window expiry, 10× window.
- Rule matcher: kind wildcard, payload `$in`/`$eq`/`$ne`/`$exists`,
  `windowDays` enforcement, `cap` ceiling.
- Correction chain: A ← corrected_by(B) → de-applied; A ←
  corrected_by(B) ← corrected_by(C) → only A counted; corrected_by
  with no target → ignored, log warning.
- Breakdown cap: 200 activities × cap=100 → exactly 101 entries
  (100 + 1 aggregate).

**Service** (`services/score_service_test.go`):

- Eager: insert activity → snapshot reflects contribution.
- Eager respects `scoreEagerOnInsert=false`.
- Profile invalidation: `Save` → every snapshot for the profile is
  `stale=true`.
- Per-tenant mutex prevents lost updates under concurrent inserts.

**Auto-emission** (`services/autoemit/*_test.go`):

- AddTag emits one `tag_added` activity with the correct
  `refs`/payload.
- ImportService commit emits one `imported` per touched person/org.
- Listener panic does not roll back the source mutation; error is
  logged.

**Handlers** (`handlers/*_test.go`):

- Activity timeline: GET with `kind=email_opened` filters
  correctly; pagination cursor round-trips.
- Manual create rejects `email_opened` (not in `ManualKinds`) with
  400.
- Correct endpoint inserts a `corrected_by` row with the right
  refs.
- ScoreProfile PATCH increments version, marks snapshots stale.
- Leaderboard sorts by value desc; `applicable=false` snapshots
  excluded.

**Repository** (`repository/*_test.go`):

- Activity dedupKey unique enforced; second insert with same key
  is a no-op success.
- HardDeleteByPersonUUID cascades.
- Snapshot upsert respects the unique `(tenant, person, profile)`
  key.

Coverage target: ≥70% on services + handlers, ≥85% on
`scoring/` (pure functions).

---

## 10. Risks + open questions

- **Activity insert volume.** Eager-on-insert means every
  activity create triggers N profile recomputes (one per active
  profile). Typical tenant has 2-6 profiles → 2-6 snapshot upserts
  per activity insert. At ~50 activities/person/year × 10k persons
  = 500k inserts/year = ~57/hour → trivial. **But** import bursts
  could spike 10k+ activities in seconds. **Mitigation**: the
  per-`(tenant, person)` mutex serializes per-person work; eager
  can be turned off per-tenant via `scoreEagerOnInsert=false` for
  large bulk imports, with the nightly job catching up.
- **`orgUuid` denormalization at write.** The schema says the
  service reads the person's primary+active membership at write
  time and stamps `orgUuid`. If the person has no memberships,
  `orgUuid` stays empty — the aggregated-by-org query in §3.1
  index list returns nothing for that activity, which is correct
  (no org engagement to attribute). Confirmed against schema doc
  §note.
- **Profile filter on Person fields.** The engine reads the
  Person's `tags`/`customFields` at compute time to evaluate
  `ProfileFilter`. That's a second DB hit per recompute. **Acceptable**
  — fetched once per `recomputeOne` call, not once per activity.
  For Phase 2's volume the cost is negligible.
- **`corrected_by` UI gap.** The API exists (POST
  `/activities/{uuid}/correct`) but the UI doesn't expose it in
  Phase 2 (the contact detail page shows the timeline read-only
  with no "correct this" affordance — Phase 3's review queue page
  is the right place). Documented as Phase 3 dependency in the
  Risks of `project_marketing_addon_phase2` memory.
- **Activity kind extension.** Static enum means the
  `agents`-addon's future `agent_interaction` kind requires a
  patch to `models/activity_kinds.go` in the marketing module. We
  accepted that trade-off (resolved decision); if it becomes
  painful, the runtime-registry path is a single-PR follow-up.
- **`marketing_score_snapshots_history`.** Out of scope. The
  current snapshot doc represents "now". Audit / time-travel for
  past values is a Phase 2.5 or Phase 3 add — until then, the
  engine's `asOf` parameter is the workaround (recompute
  on-the-fly with a past `asOf`).
- **Stripe-style idempotency on POST `/activities`.** The
  `dedupKey` unique index gives DB-level idempotency for any
  caller that hashes the same `(person, kind, occurredAt,
  externalId)`. The manual create path generates a UUID for
  `externalId` on every call, so manual re-submits create
  duplicates by design — operators can use the correct endpoint
  to roll one back. Future automated callers (webhooks, ESP
  bridges) MUST set `externalId` for replay safety.

---

## 11. Stepwise execution checklist

To track during implementation. Each item maps to one of the 5
sub-PRs in §1.

**PR-1 — data layer**

- [ ] `git checkout -b feature/marketing-phase-2` off latest `dev`.
- [ ] Extend `models/collections.go` with the 3 new constants.
- [ ] Add models (`activity.go`, `activity_kinds.go`,
  `score_profile.go`, `score_snapshot.go`).
- [ ] Repos (`activity_repo.go`, `score_profile_repo.go`,
  `score_snapshot_repo.go`) — append-only on activity_repo, all
  via `tenantrepo`.
- [ ] Extend `MarketingModule.Collections()` with the 3 collections
  + their indexes.
- [ ] Repo tests pass.

**PR-2 — pure engine**

- [ ] `scoring/` package + 4 files (engine, decay, match,
  correction).
- [ ] `engine.Compute(activities, profile, asOf) ScoreResult`
  fully implemented.
- [ ] Tests for every decay × age bucket; correction chain;
  breakdown cap; profile filter mismatch.
- [ ] Coverage ≥85% on `scoring/`.

**PR-3 — eager + nightly**

- [ ] `services/score_service.go` with `OnActivityInserted`,
  `recomputeOne`, `RecomputeStaleBatch`, `InvalidateProfile`.
- [ ] Per-tenant mutex map.
- [ ] `jobs/recompute_job.go` cloned from
  subscriptions' `renewal_job.go` shape.
- [ ] `MarketingModule.Start`/`Stop` wired.
- [ ] Add `Permissions()` for `score_profile.write` (other 2
  arrive in PR-4).
- [ ] Extend `ConfigSchema()` with the 3 new keys.
- [ ] Service-level tests pass (eager, nightly, invalidation,
  concurrency).

**PR-4 — API + auto-emission**

- [ ] Handlers: `activity_handler.go`, `score_profile_handler.go`,
  `snapshot_handler.go`.
- [ ] Route registration in `module.go::RegisterRoutes` — 3 new
  permission buckets.
- [ ] `services/autoemit/` with `PersonListener`, `TagListener`,
  `ImportListener`.
- [ ] `PersonService` / `TagService` / `ImportService` grow
  `RegisterListener(...)`; existing call sites untouched.
- [ ] Activity emission on the 4 Phase-1 mutations: person
  create/merge, tag add/remove, import commit.
- [ ] Cedar policy file `policies/marketing.cedar` includes the
  new keys.
- [ ] Manual smoke: create 3 activities, see one snapshot per
  active profile populated; leaderboard returns sorted rows.
- [ ] `make openapi-dump` regen; commit the diff.
- [ ] Handler tests pass.

**PR-5 — frontend + docs**

- [ ] `pages/marketing/scoring/` page (list + form + leaderboard).
- [ ] `pages/marketing/contacts/detail` grows `?tab=timeline` and
  `?tab=scores` (URL-synced per the `url-tabs` skill).
- [ ] Manual-activity modal on contact detail.
- [ ] RTK Query slice extended.
- [ ] EN + IT locale strings added.
- [ ] `make ci-frontend-admin` green.
- [ ] `addons/marketing/CLAUDE.md` updated (Phase 2 → shipped).
- [ ] Memory pointer in `MEMORY.md` +
  `project_marketing_addon_phase2.md` created.
- [ ] PR `feature/marketing-phase-2 → dev` with a rollup
  description.

---

*Plan owner: claude. Track Phase 2 status here; spawn separate
plans for Phase 3 (excel/odoo + conflict review queue), Phase 4
(cards), Phase 5 (campaigns) once Phase 2 is in `dev`.*
