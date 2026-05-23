---
type: implementation-plan
author: claude
date: 2026-05-22
domain: orkestra
component: marketing-addon
status: ready-to-execute
phase: 4 (Card lifecycle)
branch: feature/marketing-phase-4
---

# Marketing Addon — Phase 4 Implementation Plan

Concrete plan to land **Phase 4 (Card / membership lifecycle)** from
[`Orkestra_marketing_addon.md`](Orkestra_marketing_addon.md) §9 on
top of `dev` at `bf1f362` (Phase 2 + Phase 3 marketing rollup
merged via PR #50 on 2026-05-22).

Phase 1 reference plan: [`IMPLEMENTATION_PLAN.md`](IMPLEMENTATION_PLAN.md).
Phase 2 reference plan: [`IMPLEMENTATION_PLAN_PHASE_2.md`](IMPLEMENTATION_PLAN_PHASE_2.md).
Phase 3 reference plan: [`IMPLEMENTATION_PLAN_PHASE_3.md`](IMPLEMENTATION_PLAN_PHASE_3.md).
Phase 5 (marketing operativo — segments, lead-capture, campaigns,
ESP webhooks) is explicitly **out of scope** and will be planned
separately once Phase 4 is in `dev`.

Decisions resolved with the user before this plan was written
(2026-05-22):

- **Branch base**: `dev` (already updated to include Phase 2 + Phase 3
  + the x/crypto v0.52.0 bump). Phase 4 is the first marketing phase
  branched off a clean `dev` since Phase 1; no stacking with Phase 3.
- **PR split**: 5 sequential sub-PRs on `feature/marketing-phase-4`
  (data layer + code-format generator, card lifecycle services +
  audit + expiration scheduler, HTTP + Cedar, Phase-3 leftovers
  backend, frontend + docs). Mirrors Phase 3's shape.
- **Activity kinds**: two per spec §6.2 — `card_issued` on emit;
  `card_status_changed` on every transition (suspend / reinstate /
  revoke / expire) with payload `{from, to, reason}`. The enum
  constants are already declared in `models/activity_kinds.go` from
  Phase 2 — Phase 4 only fills the payload semantics.
- **Phase-3 leftovers pulled into Phase 4**: all three explicitly
  agreed in scope —
  - **Engagement-CSV row emission** (detector ships Phase 3, the
    `csv.source.Run` emit path was deferred).
  - **Soft-match scan activation** in pipeline strict-miss path
    (helpers ship Phase 3, the integration + required Person /
    Organization indexes were deferred).
  - **`corrected_by` UI flow** (backend ships Phase 2 +
    `ListCorrectionsForActivity` helper added Phase 3; UI with
    audit trail + strike-through display was deferred).
- **Plan language**: English, parity with Phase 2 + Phase 3.

## Phase 4 deliverables (from §9 + leftovers)

What ships at the end of this plan:

### Card lifecycle (the core of §9 Phase 4)

- 2 new MongoDB collections — `marketing_card_types` (templates) +
  `marketing_cards` (instances). Schemas already canonical at
  [`schemas/marketing_card_types.md`](schemas/marketing_card_types.md)
  and [`schemas/marketing_cards.md`](schemas/marketing_cards.md).
- 1 internal helper collection — `marketing_card_sequences` (atomic
  per-(tenantId, cardTypeUuid) counter for the `{seq:N}` placeholder
  in `code_format`). Not in the design doc's collection list because
  it is an implementation detail of code generation, not a domain
  entity — operators never see it, and rebuilding it from
  `max(card.code)` is straightforward if it is ever lost.
- **Code-format template grammar** (`services/card_code_format.go`)
  with five placeholders per the schema's `code_format` note:
  - `{YYYY}` / `{YY}` / `{MM}` / `{DD}` — components of the current
    UTC date stamped at emit time.
  - `{seq:N}` — zero-padded counter per (tenantId, cardTypeUuid),
    advanced via `findAndModify` on `marketing_card_sequences`.
  - `{rand:N}` — N alphanumeric characters from a 32-symbol
    Crockford-Base32 alphabet (no I/L/O/U) drawn from
    `crypto/rand`. Used for non-predictable codes.
  - Literal text between placeholders passes through verbatim. The
    grammar is validated at card-type create / update time
    (`CardTypeService.Validate`), and a fail-safe unique index on
    `(tenantId, code)` ensures the second writer of a colliding code
    loses with `E11000` (caller retries on conflict).
- **Card lifecycle service** (`services/card_service.go`) implementing
  the §7.3 state machine:
  - `Issue(ctx, personUuid, cardTypeUuid, tier, opts) (*Card, error)` —
    checks `allow_multiple_per_person` constraint via the
    partial-unique index probe, generates `code`, writes the
    `marketing_cards` row with `status = active`, atomically adds
    the new uuid to `marketing_persons.activeCardUuids`, emits
    `KindCardIssued`.
  - `Suspend(ctx, cardUuid, reason)` — `active → suspended`,
    reversible. Atomic Person update **does not** remove from
    `activeCardUuids` (decision §3.4 below — `activeCardUuids` is
    a denorm of "issued and not revoked", suspended cards stay in
    the list with `status = suspended`).
  - `Reinstate(ctx, cardUuid)` — `suspended → active`. Clears
    `suspended_at` / `suspended_by` / `suspend_reason`. Emits
    `card_status_changed` with `from=suspended, to=active`.
  - `Revoke(ctx, cardUuid, reason)` — terminal. Sets `revoked_*`,
    atomically `$pull`s the uuid from `Person.activeCardUuids`.
    Emits `card_status_changed` with `from=<prev>, to=revoked,
    reason=<reason>`.
  - `Expire(ctx, cardUuid)` — internal, called only by the
    expiration scheduler. Same write path as `Revoke` with
    `reason = "expired"`. Emits `card_status_changed` with
    `from=<prev>, to=revoked, reason=expired`.

  Every transition is gated by the matrix:

  ```
  from \ to    | active | suspended | revoked
  -------------+--------+-----------+--------
  (new)        |   ✓    |     ✗     |   ✗
  active       |   —    |     ✓     |   ✓
  suspended    |   ✓    |     —     |   ✓
  revoked      |   ✗    |     ✗     |   ✗  (terminal)
  ```

  Illegal transitions return `errcode.UnprocessableEntity(errcode.
  MarketingCardInvalidTransition, ...)`.

- **Expiration scheduler** — adds a `cardScheduler` field on the
  module, started in `Start()`, stopped in `Stop()`. Ticker driven
  by the new `card.expiration_check_cron` config (default
  `@every 1h`). On each tick, scans active cards across all tenants
  with `expires_at < now()` via the existing
  `(tenantId, status, expires_at)` sparse index, re-stamps `tenantId`
  onto the worker ctx, and calls `CardService.Expire` for each.
  Cross-tenant bypass goes through a new
  `CardRepository.ListExpiringAcrossTenants(asOf time.Time)`
  method, documented in the same loud-comment style as
  `ScoreSnapshotRepository.ListStaleAcrossTenants` and
  `ImportJobRepository.ListAcrossTenantsByStatus`. **Third
  documented bypass** in the marketing addon — the pattern is
  load-bearing for any background scanner.

- **HTTP routes** (PR-3 detail in §6):
  - 5 card-type routes under `marketing.card_type.write` for
    write, `marketing.contact.read` for read.
  - 5 card routes under three new `marketing.card.{issue,
    suspend, revoke}` permission keys.

- **3 new Cedar permission keys**:
  - `marketing.card_type.write` — create / update / disable a card
    type. Reads fold into `marketing.contact.read`.
  - `marketing.card.issue` — emit a new card to a Person.
  - `marketing.card.suspend` — suspend or reinstate an active card
    (suspend + its inverse are the same bucket).
  - `marketing.card.revoke` — terminal revoke (separate bucket from
    suspend because revoke is irreversible and ops typically
    constrains it tighter).

  After Phase 4 the addon declares 10 permission keys total.

- **Filter additions** on `GET /v1/marketing/persons`:
  - `?hasActiveCard=true|false` — filter by non-empty
    `activeCardUuids` (matches §7.5).
  - `?activeCardOfType=<cardTypeUuid>` — filter by
    `activeCardUuids` containing any card of the given type, via a
    lookup against `marketing_cards` by `(card_type_id, status =
    active)`.

  Both lift Phase 4's card-issuance into the existing contacts
  list without forcing operators into the new card admin pages
  just to slice the contact base.

### Phase-3 leftovers (the three the user pulled into scope)

- **Engagement-CSV row emission** —
  `importers/csv/source.go::Run` consults
  `engagement.DetectColumns` (already in tree as of Phase 3) and,
  when the `engagementMode` flag is set on the run config, emits
  one `models.Activity` per row keyed on the detected column. The
  emit funnel is the existing `services/activity_emit.go`
  `ActivityEmitter` interface — same dedup-key invariant as the
  pipeline-driven `imported` / `merged` rows. New flag
  `ImportConfig.EngagementMode bool` (default false; opt-in via the
  wizard checkbox). Tests cover one row producing one `email_opened`
  activity with `occurredAt` parsed from the `occurred_at` column.

- **Soft-match scan activation** in pipeline strict-miss path —
  adds three indexes via `Collections()`:
  - `marketing_persons`: `{tenantId: 1, firstNameLower: 1,
    lastNameLower: 1}` btree, only present when both first +
    last are set (sparse via a service-layer write).
  - `marketing_persons`: `{tenantId: 1, phoneLast10: 1}` multikey
    (denormalized last-10-digits of every phone).
  - `marketing_organizations`: `{tenantId: 1, legalNameNormalized:
    1}` unique-sparse.

  The denormalised fields (`firstNameLower`, `lastNameLower`,
  `phoneLast10[]`, `legalNameNormalized`) are populated by the
  service layer on Create / Update of the Person / Organization;
  the `repository/normalize.go` helpers from Phase 1 already cover
  the casing + phone-digit transforms — extending them is two
  small functions. Pipeline integration: on a strict-key miss the
  pipeline calls `match.SoftMatchPerson` /
  `match.SoftMatchOrganization`, and on a soft-match hit parks the
  row in `marketing_conflict_reviews` (status = `pending`,
  matchKind = `soft_match`, same shape as Phase 3 hard-conflict
  rows). The `ReviewResolverModal` already badge-renders
  `soft_match` so the UI piece is free.

- **`corrected_by` UI flow** — exposes the existing backend
  (`POST /v1/marketing/activities/{id}/correct`) on the contact-
  detail Timeline tab. Each activity row gains a Correct button →
  modal with a free-form `reason` textarea + a confirmation step;
  on submit the new `corrected_by` activity is inserted with
  `refs.correctsActivityUuid` pointing at the original. The
  Timeline list refresh marks the original with a strikethrough
  and renders an inline "↻ corrected · view note" badge that
  expands the corrector's `reason` payload. Reads use the
  `ListCorrectionsForActivity` helper added during Phase 3.

## 1. Branch + workflow

```bash
# Already executed at session start:
git checkout dev && git pull
git checkout -b feature/marketing-phase-4
git push -u origin feature/marketing-phase-4
```

Branched off `dev` at `bf1f362` (the Phase 2 + Phase 3 marketing
rollup merge). Unlike Phase 3, Phase 4 does **not** stack on a
prior phase branch — its dependencies (`marketing_activities`,
`ActivityService`, `ActivityEmitter`, the conflict-review pipeline)
all live on `dev`.

Sub-PR strategy onto `feature/marketing-phase-4` (each independently
green on CI; merge to branch, not yet to `dev`):

1. **PR-1**: data layer + code-format generator —
   `marketing_card_types`, `marketing_cards`,
   `marketing_card_sequences` collections + repositories, the
   template-grammar parser + sequence counter, Person field
   wire-up (denormalized `firstNameLower` / `lastNameLower` /
   `phoneLast10` for the Phase-3-leftover soft-match indexes, plus
   the existing `activeCardUuids` is already reserved). No
   business logic yet — just the persistence shape and the index
   build.
2. **PR-2**: card lifecycle services + expiration scheduler +
   audit emission — `CardTypeService`, `CardService` with the
   full state-machine matrix, the cross-tenant
   `ListExpiringAcrossTenants` bypass, `cardScheduler` ticker, and
   the `card_issued` / `card_status_changed` activity emission.
   No HTTP routes yet (PR-3) but the services are integration-
   tested against a real in-memory pipeline.
3. **PR-3**: HTTP + Cedar — 10 new handlers (5 card-type, 5 card),
   3 new permission keys, the `?hasActiveCard=...` /
   `?activeCardOfType=...` query params on the existing persons
   list. OpenAPI dump regenerated (see §9.2 below — preflight
   item).
4. **PR-4**: Phase-3 leftovers backend — wire the engagement-CSV
   emitter into `csv.source.Run`, integrate
   `match.SoftMatchPerson` / `SoftMatchOrganization` into the
   pipeline strict-miss path, add the three soft-match-supporting
   indexes, polish `ListCorrectionsForActivity` for the UI's
   needs.
5. **PR-5**: frontend (card-type admin page, contact-detail Cards
   tab + Issue / Suspend / Reinstate / Revoke modals, Timeline
   Correct button + strikethrough rendering, engagement-mode
   checkbox on the import wizard) + RTK Query slice extension +
   EN+IT i18n + CLAUDE.md + memory file + OpenAPI dump.

Each PR opens against `feature/marketing-phase-4`; the rollup to
`dev` happens after PR-5 lands and CI is green on the integrated
branch.

---

## 2. Module additions (PR-1 wiring)

### 2.1 Collection constants

Extend `models/collections.go` with the three new names:

```go
const (
    // ...Phase 1 + 2 + 3 entries...

    // Phase 4 — Card lifecycle.
    CardTypesCollection     = "marketing_card_types"
    CardsCollection         = "marketing_cards"
    CardSequencesCollection = "marketing_card_sequences"
)
```

### 2.2 Collections() index declarations

Append three blocks to the module's `Collections()`:

```go
{
    Name: models.CardTypesCollection,
    Indexes: []module.IndexSpec{
        {Keys: bson.D{{"tenantId", 1}, {"key", 1}}, Unique: true},      // §schema unique-per-tenant
        {Keys: bson.D{{"tenantId", 1}, {"active", 1}}},                 // "list active types"
        {Keys: bson.D{{"tenantId", 1}, {"updatedAt", -1}}},
    },
},
{
    Name: models.CardsCollection,
    Indexes: []module.IndexSpec{
        {Keys: bson.D{{"tenantId", 1}, {"code", 1}}, Unique: true},               // fail-safe code uniqueness
        {Keys: bson.D{{"tenantId", 1}, {"personUuid", 1}}},                       // all cards of a person
        {Keys: bson.D{{"tenantId", 1}, {"cardTypeUuid", 1}, {"status", 1}}},      // "all active premium"
        {Keys: bson.D{{"tenantId", 1}, {"tier", 1}, {"status", 1}}, Sparse: true},
        {Keys: bson.D{{"tenantId", 1}, {"status", 1}, {"expiresAt", 1}}, Sparse: true}, // expiration scan
        {Keys: bson.D{{"tenantId", 1}, {"issuedAt", -1}}},
    },
},
{
    Name: models.CardSequencesCollection,
    Indexes: []module.IndexSpec{
        {Keys: bson.D{{"tenantId", 1}, {"cardTypeUuid", 1}}, Unique: true},
    },
},
```

The schema's "one Active=true per (personUuid, cardTypeUuid) when
`card_type.allow_multiple_per_person = false`" is **not** declared
as a partial-unique index — the SDK's `IndexSpec` still does not
expose `PartialFilterExpression` (same limitation Phase 1
documented for Membership invariants). It is enforced at the
service layer via a pre-write probe on the
`(tenantId, cardTypeUuid, status = active)` index (atomic enough
for sane operator concurrency; a race produces a duplicate which
the next operator review catches at the contact-detail Cards tab).
Widening the SDK is still tracked as the Phase-2 follow-up.

### 2.3 Permissions

Three new keys on `module.Permissions()`:

```go
{Key: "marketing.card_type.write",
 Description: "Create / update / disable card types."},
{Key: "marketing.card.issue",
 Description: "Emit a new marketing card to a person."},
{Key: "marketing.card.suspend",
 Description: "Suspend or reinstate an active card."},
{Key: "marketing.card.revoke",
 Description: "Revoke a card (terminal; irreversible)."},
```

Reads of `card_types` + `cards` fold into `marketing.contact.read`
(same fold-in pattern Phase 2 used for activities / snapshots and
Phase 3 used for the conflict-review queue — granting "see who
holds a Gold card" without "see contacts" is incoherent for any
realistic role catalog).

### 2.4 ConfigSchema

One new config key:

```go
{Key: "card.expiration_check_cron", Type: "string",
 Default: "@every 1h",
 EnvVar: "MARKETING_CARD_EXPIRATION_CRON",
 Description: "Cron expression for the card expiration scanner. " +
              "Cards with status=active AND expires_at <= now() " +
              "are auto-revoked with reason='expired' on each tick. " +
              "Set to empty string to disable the scanner."},
```

The other §3.5 card-related parameters (per-type name, tier,
`code_format`, default benefits, allow-multiple) live on the
`marketing_card_types` documents themselves — they are per-type,
not per-tenant defaults.

### 2.5 NavItems

Append a "Card Types" child under the existing Marketing nav group:

```go
{Group: "Marketing", Label: "Card Types", Path: "/marketing/card-types",
 Icon: "fa-id-card", MinRole: "manager"},
```

Sidebar order in the Marketing group after Phase 4:
Contacts · Tags · Custom Fields · Imports · Reviews ·
**Card Types** · Scoring.

The contact-detail Cards tab is reached via the contact detail
route (`/marketing/persons/:uuid`) — no separate nav entry.

### 2.6 Init() wiring

Phase 4 services hook into `Init()` between the Phase 3 importer /
review wiring and the existing scoring scheduler:

```go
m.CardTypeRepo  = repository.NewCardTypeRepository(deps.Mongo)
m.CardRepo      = repository.NewCardRepository(deps.Mongo)
m.CardSeqRepo   = repository.NewCardSequenceRepository(deps.Mongo)

m.CardTypeService = services.NewCardTypeService(services.CardTypeDeps{
    TypeRepo: m.CardTypeRepo,
})

m.CardService = services.NewCardService(services.CardDeps{
    TypeRepo:        m.CardTypeRepo,
    CardRepo:        m.CardRepo,
    SeqRepo:         m.CardSeqRepo,
    PersonRepo:      m.PersonRepo,
    ActivityService: m.ActivityService,           // Phase 2 — for card_issued / card_status_changed
    Clock:           clockwork.NewRealClock(),    // injected for testability
    RandReader:      crypto_rand.Reader,          // injected for the {rand:N} placeholder
})

m.CardScheduler = services.NewCardScheduler(services.CardSchedulerDeps{
    CardRepo:    m.CardRepo,
    CardService: m.CardService,
    Cron:        settings.CardExpirationCron,
    Logger:      deps.Logger,
})
```

`Start()` calls `m.CardScheduler.Start(ctx)`; `Stop()` calls
`m.CardScheduler.Stop(ctx)`. The scheduler honours the
`CardExpirationCron` setting being the empty string by skipping
the ticker entirely (matches the Phase 2 score scheduler's
behaviour for `score.recompute_cron == ""`).

### 2.7 ProvidedServices / cross-module surface

Phase 4 does **not** expose any new `pkg/sdk/iface` interface.
Card lifecycle is internal to the marketing addon — no other
addon reads cards programmatically in the foreseeable roadmap.
The HTTP surface is the contract.

If a future addon needs to react to card lifecycle events
(e.g. `notification` mailing a welcome email on `card_issued`)
the natural seam is the existing `marketing_activities` log —
`notification` can subscribe via a not-yet-existing
`ActivityService.RegisterInsertListener` hook (Phase 5 work,
deferred — the seam already exists in `services/activity_service.go`
for the score engine's eager-recompute call).

---

## 3. Card model + lifecycle details

### 3.1 `marketing_card_types` document

Per [`schemas/marketing_card_types.md`](schemas/marketing_card_types.md).
Notable encoding choices for the implementation:

- `key` is normalised lowercase + ASCII-slugified at write time
  via the existing `services/slug.go` helper (introduced Phase 1
  for tags). Operator input "Premium Member" → `premium_member`.
- `code_format` is parsed eagerly at create / update time via
  `services/card_code_format.go::Parse`. Returns an AST that the
  card service caches per type (no parse on every emit).
- `tiers` is stored as a `[]string` with no enforced
  case-folding — the operator's intent ("standard" vs "Standard")
  is preserved. Service-layer validation rejects a card-issue
  whose tier is not in `card_type.tiers` (exact-match).
- `allow_multiple_per_person`'s flip from `true` to `false` does
  **not** retroactively close duplicates — the schema note covers
  this. The admin UI shows a warning when the operator toggles
  it down with existing duplicates.

### 3.2 `marketing_cards` document

Per [`schemas/marketing_cards.md`](schemas/marketing_cards.md).
Notable encoding choices:

- `status` is a typed `CardStatus` string enum
  (`active` / `suspended` / `revoked`) declared in
  `models/card_status.go` alongside an `AllStatuses` slice and
  `IsKnownStatus` predicate (matches the pattern Phase 2 used for
  `ActivityKind`).
- `benefits` is a snapshot — taken from `card_type.default_benefits`
  at emit time, then editable per instance. Mutations on the type
  do not propagate to existing cards (the schema § notes this).
- `expires_at` is **optional** — `nil` means "no scheduled
  expiration". The scheduler's index is `Sparse: true` so absent
  `expires_at` rows are not even visited.
- `suspended_*` and `revoked_*` field families are cleared on
  reinstate (`$unset`) so the document never carries stale
  suspension metadata after a recovery.

### 3.3 `marketing_card_sequences` document

```go
type CardSequence struct {
    UUID         string    `bson:"uuid"`
    TenantID     string    `bson:"tenantId"`
    CardTypeUUID string    `bson:"cardTypeUuid"`
    NextSeq      int64     `bson:"nextSeq"`
    UpdatedAt    time.Time `bson:"updatedAt"`
}
```

`CardSequenceRepository.NextSequence(ctx, cardTypeUuid)` is a
`findAndModify` with `$inc: {nextSeq: 1}` and `upsert: true`,
returning the **pre-increment** value so the first issue of a type
gets `seq = 0` (caller can zero-pad to whatever width the
`{seq:N}` placeholder demands). No retries — Mongo's
`findAndModify` is atomic, and lost-update races are impossible by
construction.

### 3.4 `marketing_persons.activeCardUuids` semantics

Decision recorded here (the schema docs §41 row only states the
denorm exists; the semantics are not explicit):

`activeCardUuids` lists every card uuid for that person where
`status ∈ {active, suspended}` — i.e. "card is issued and not
revoked". `Suspend` does **not** remove from the list; `Revoke`
and `Expire` **do**.

Rationale:

- A suspended card is still a held asset (operator may reinstate).
  Treating suspension as "removed from list" hides existing
  inventory and would require the contact-detail Cards tab to do
  a parallel fetch on a different filter to surface it.
- Revoke is terminal — the card cannot come back, so the list
  reflects the user's current holdings cleanly.
- Filter semantics on `?hasActiveCard=true` therefore mean "the
  person has an issued, non-revoked card." When operators need
  "only currently-usable cards" (e.g. for gating), the filter is
  expressed at the `marketing_cards` collection level on
  `status=active`, not via `activeCardUuids`.

`CardService.Suspend` therefore writes only the `marketing_cards`
row + emits the activity; `activeCardUuids` is untouched.
`CardService.Revoke` / `Expire` write the row + `$pull` from
`Person.activeCardUuids` + emit the activity. All three updates
on revoke are issued in a single Mongo transaction (already
required for the activity emission to be atomic with the card
status flip).

### 3.5 Code-format grammar

```ebnf
template     = { literal | placeholder } ;
placeholder  = "{" ( datepart | seq | rand ) "}" ;
datepart     = "YYYY" | "YY" | "MM" | "DD" ;
seq          = "seq" ":" digit+ ;
rand         = "rand" ":" digit+ ;
literal      = any char except "{"  ;
```

Validation rules at parse time:

- At most one `{seq:N}` placeholder per template — multiple
  sequences in the same code are nonsensical (no operator UX
  story).
- `N ≥ 1` and `N ≤ 12` for both `{seq:N}` and `{rand:N}` — beyond
  12 the code crosses MongoDB's natural readability boundary
  without adding entropy that operators would actually verify.
- Unknown placeholder names (e.g. `{foo}`) fail parse with a
  descriptive error pointing at the offending byte offset.
- The empty template is rejected.

Emission semantics:

- The current UTC date is captured **once** per `Issue` call (not
  per placeholder) so a multi-placeholder template like
  `{YYYY}-{MM}-{seq:05}` always carries a consistent date.
- `{seq:N}` is rendered as zero-padded decimal. If the counter
  exceeds `10^N - 1`, the rendered code is wider than N
  characters — the unique index catches the collision, the
  caller logs a warning, and operators move to a wider template.
- `{rand:N}` reads N bytes from `crypto/rand`, maps each into
  the 32-symbol Crockford-Base32 alphabet via modulo bias safe
  for cryptographic strength at N ≤ 12. The test in
  `services/card_code_format_test.go` covers distribution shape
  (chi-square against uniform over 10^5 samples).

### 3.6 State-machine implementation

`CardService` keeps the transition matrix in a single
`canTransition(from, to CardStatus) bool` function reachable from
both the public `Suspend` / `Reinstate` / `Revoke` / `Expire`
methods and the unit-test matrix that walks every pair. Illegal
attempts return `errcode.UnprocessableEntity(errcode.
MarketingCardInvalidTransition, ...)` with a `detail` describing
the attempted edge. The two new `errcode` constants
(`MarketingCardInvalidTransition`, `MarketingCardCodeCollision`)
land in `internal/shared/errcode/codes.go` per the contract test.

---

## 4. PR-1 details — data layer + code-format generator

**Goal**: persistence + utility ready, no business behaviour yet.
Reviewer-friendly small surface (~600–800 lines new code, ~200
test, plus the 3 new collection blocks in `module.go`).

### 4.1 New files

```
backend/internal/addons/marketing/
├── models/
│   ├── card_type.go              # CardType struct
│   ├── card.go                   # Card struct
│   ├── card_status.go            # CardStatus enum + AllStatuses
│   └── card_sequence.go          # CardSequence struct
├── repository/
│   ├── card_type_repo.go         # tenantrepo.Scope on every read; Insert / Update / List / FindByUUID / FindByKey
│   ├── card_type_repo_test.go
│   ├── card_repo.go              # adds ListExpiringAcrossTenants (documented cross-tenant bypass)
│   ├── card_repo_test.go
│   ├── card_sequence_repo.go     # NextSequence via findAndModify $inc upsert
│   └── card_sequence_repo_test.go
└── services/
    ├── card_code_format.go       # Parse → AST; Render(ast, ctx) → string
    └── card_code_format_test.go  # grammar tests + chi-square on {rand:N}
```

### 4.2 Modified files

- `models/collections.go` — three new const lines (§2.1).
- `module.go` — three new `Collections()` blocks (§2.2). No
  permission / nav / service wiring yet — PR-2 covers those.
- `internal/shared/errcode/codes.go` — two new constants
  (`MarketingCardCodeCollision`,
  `MarketingCardInvalidTransition`). Update the golden-file test
  baseline in the same commit.

### 4.3 Verification

- `make ci-backend` clean.
- New collections auto-created on a fresh boot (verified by
  starting the backend container against an empty Mongo and
  `db.marketing_card_types.getIndexes()`).

---

## 5. PR-2 details — card lifecycle services + scheduler + audit

**Goal**: every state transition fires through one funnel, every
write emits the audit activity, and the expiration scanner runs.

### 5.1 New files

```
backend/internal/addons/marketing/
├── services/
│   ├── card_type_service.go        # CRUD + Validate(code_format AST)
│   ├── card_type_service_test.go
│   ├── card_service.go             # Issue / Suspend / Reinstate / Revoke / Expire
│   ├── card_service_test.go        # full state-machine matrix
│   └── card_scheduler.go           # ticker + ListExpiringAcrossTenants drain
```

### 5.2 Modified files

- `module.go` — Init() wires Phase 4 services (§2.6); Start() /
  Stop() lifecycle on the scheduler.
- `repository/person_repo.go` — adds
  `AddActiveCard(ctx, personUuid, cardUuid)` ($addToSet) and
  `RemoveActiveCard(ctx, personUuid, cardUuid)` ($pull) helpers
  consumed by `CardService`.

### 5.3 Cross-tenant bypass — documented

`CardRepository.ListExpiringAcrossTenants(ctx, asOf time.Time)`:

```go
// ListExpiringAcrossTenants is one of THREE documented
// cross-tenant bypasses in this package, alongside
// ScoreSnapshotRepository.ListStaleAcrossTenants (Phase 2,
// nightly score recompute) and
// ImportJobRepository.ListAcrossTenantsByStatus (Phase 3,
// worker boot recovery). The card expiration scheduler runs as
// a background ticker with no inbound tenant ctx, so the read
// path must scan every tenant. The caller (CardScheduler.tick)
// re-stamps tenantId onto a fresh ctx via ctxauth.KeyTenantID
// before invoking CardService.Expire — every downstream write
// is scoped normally.
//
// DO NOT GENERALIZE THIS PATTERN. Every new cross-tenant
// background job earns one targeted method here, not a generic
// bypass helper.
func (r *cardRepository) ListExpiringAcrossTenants(
    ctx context.Context, asOf time.Time,
) ([]models.Card, error) { ... }
```

### 5.4 Audit emission

Both kinds use the existing `ActivityEmitter`
(`services/activity_emit.go`) — Phase 4 does not add to the
emitter interface, only consumes it. Payload shapes:

```jsonc
// card_issued
{
  "cardUuid": "...",
  "cardTypeUuid": "...",
  "cardTypeKey": "premium_member",
  "code": "PREM-2026-00042",
  "tier": "gold",        // omitted if no tier
  "expiresAt": "..."     // omitted if nil
}

// card_status_changed
{
  "cardUuid": "...",
  "cardTypeUuid": "...",
  "from": "active",
  "to": "suspended",
  "reason": "policy violation"   // free-form operator text or "expired" from the scheduler
}
```

Both rows carry `source = "system"` (per the
`ActivitySourceSystem` constant) and `recordedAt = now()`,
matching Phase 3's `imported` / `merged` / `tag_added` /
`tag_removed` emission shape.

### 5.5 Verification

- `make ci-backend` clean, including the new state-machine table
  test (every illegal `(from, to)` returns the typed error;
  every legal pair succeeds and emits the right activity).
- Scheduler dry-run: forced tick with one expired card produces
  one `card_status_changed` activity with `reason = "expired"`
  and the card moves to `revoked` + leaves `activeCardUuids`.

---

## 6. PR-3 details — HTTP + Cedar + persons-list filters

**Goal**: 10 new handlers, 3 new permissions, 2 new filter params,
regenerated OpenAPI dump.

### 6.1 New files

```
backend/internal/addons/marketing/handlers/
├── card_type_handler.go
├── card_type_handler_test.go
├── card_handler.go
└── card_handler_test.go
```

### 6.2 Routes

```
GET    /v1/marketing/card-types                                 # marketing.contact.read
GET    /v1/marketing/card-types/{uuid}                          # marketing.contact.read
POST   /v1/marketing/card-types                                 # marketing.card_type.write
PATCH  /v1/marketing/card-types/{uuid}                          # marketing.card_type.write
DELETE /v1/marketing/card-types/{uuid}                          # marketing.card_type.write — only when no cards of that type exist

GET    /v1/marketing/persons/{personUuid}/cards                 # marketing.contact.read
GET    /v1/marketing/cards/{uuid}                               # marketing.contact.read
POST   /v1/marketing/persons/{personUuid}/cards                 # marketing.card.issue
POST   /v1/marketing/cards/{uuid}/suspend                       # marketing.card.suspend
POST   /v1/marketing/cards/{uuid}/reinstate                     # marketing.card.suspend  (same bucket as suspend; reinstating is the inverse)
POST   /v1/marketing/cards/{uuid}/revoke                        # marketing.card.revoke
```

The two new query params on the existing persons list:

```
GET /v1/marketing/persons?hasActiveCard=true|false
GET /v1/marketing/persons?activeCardOfType=<cardTypeUuid>
```

Both layered onto the existing `personHandler.list` — backed by
`marketing_persons.activeCardUuids` (already indexed; no schema
change).

### 6.3 Modified files

- `module.go::RegisterRoutes` — 10 new handler registrations,
  three new `RequirePermission` middleware-bucketed subgroups.
- `module.go::Permissions()` — 3 new entries (§2.3).
- `handlers/person_handler.go` — extends the list query parser to
  recognise the two new params and translate them into the
  repository's filter struct.
- `repository/person_repo.go` — adds
  `WithActiveCardFilter(presence bool)` and
  `WithActiveCardOfTypeFilter(cardTypeUuid string)` builder
  methods on the existing filter struct.
- `backend/openapi/enterprise.json` — regenerated via
  `make openapi-dump` after the route changes land (per the CI
  release-blockers checklist in memory).

### 6.4 Frontend wiring — deferred to PR-5

Although the routes ship in PR-3, the RTK Query slice extension +
new modals land in PR-5. The handlers are tested end-to-end via
the Go handler tests; no frontend artifact required for PR-3 to
be merge-ready.

### 6.5 Verification

- `make ci-backend` clean, including new handler tests.
- `make openapi-dump` produces a diff matching only the new
  routes; commit the dump in the same PR.
- `gh pr checks` shows green across the profile matrix (the
  `enterprise` build is the one that actually compiles the
  marketing addon; the other SKUs skip it via build tag).

---

## 7. PR-4 details — Phase-3 leftovers backend

**Goal**: close the three Phase-3 deferrals (engagement-CSV emit,
soft-match scan, corrected_by reads) without touching the card
lifecycle code.

### 7.1 Engagement-CSV row emission

- `importers/csv/source.go::Run` reads
  `engagement.DetectColumns(mapping.Headers)` once at extract
  start. When the result is non-empty AND
  `runConfig.EngagementMode == true`, each subsequent CSV row
  produces one Activity per detected engagement column whose cell
  is truthy ("1" / "true" / "yes" / "y" / non-empty), keyed by
  the column→kind mapping in `engagement.ColumnToKind`. The
  `occurred_at` cell parses to `time.Time` via a small permissive
  parser (RFC3339, RFC3339Nano, `2006-01-02`, `2006-01-02 15:04:05`)
  and populates `Activity.OccurredAt`; the fallback when the cell
  is missing or unparseable is `time.Now().UTC()` with a job-stat
  counter `engagementOccurredAtFallback++` for operator
  visibility.
- Each emitted Activity carries `source = "importer"`,
  `dedupKey = sha256(personUuid + kind + occurredAt + jobUuid)` so
  re-imports stay idempotent.
- New config field on the existing `ImportConfig`:
  `EngagementMode bool` (JSON tag `engagementMode`, default
  false). Wire from the multipart handler's mapping JSON.
- Tests: a 3-row CSV with `email_opened` + `email_clicked` +
  `occurred_at` columns produces 6 Activities (2 per row), each
  with the right kind and parsed `occurredAt`. A second run with
  the same payload produces 0 new Activities (dedup).

### 7.2 Soft-match scan activation

- `repository/normalize.go` already has `NormalizePhone` (digit
  extract, keep last 10) and `LowercaseTrimmed` — Phase 4 adds
  `NormalizeLegalName` (lower + trim + collapse runs of
  whitespace to single space; matches the Phase 3 helper in
  `importers/match/`).
- `services/person_service.go::Create` / `Update` and
  `services/organization_service.go::Create` / `Update` populate
  the new denormalized fields:
  - Person: `firstNameLower`, `lastNameLower`, `phoneLast10[]`
    (multikey of last-10-digits across every entry in `phones`).
  - Organization: `legalNameNormalized`.
- `module.go::Collections()` extends the Person + Org index lists
  with the three new btree / multikey / unique-sparse indexes
  (§"Phase-3 leftovers" above).
- `importers/pipeline.go::dedup` — after the strict-key miss
  branch, before the create-new branch, calls
  `match.SoftMatchPerson(ctx, candidate, repo)` /
  `match.SoftMatchOrganization(...)`. On a hit, parks the
  CanonicalRecord in `marketing_conflict_reviews` with
  `matchKind = "soft_match"` and the matched existing uuid; the
  pipeline's `commitOrPark` already handles the parking flow.
- The `ReviewResolverModal` already renders `soft_match` rows
  with a "Soft match" badge — no UI change needed.
- One-shot backfill job: a small `services/soft_match_backfill.go`
  that walks every existing Person + Organization and computes
  the new denormalised fields. The job is invoked once at PR-4
  boot via a sentinel doc in `marketing_module_state` ("schema
  versions") so subsequent boots skip it. Keep it idempotent and
  small (~150 lines).
- Tests: importing a second time with first+last+phone overlap
  on a different email produces 1 parked review (not a duplicate
  Person); the resolution modal applies `take_incoming` and the
  pipeline merges correctly.

### 7.3 corrected_by service polish

- `services/activity_service.go::ListCorrectionsForActivity(ctx,
  activityUuid)` — refines the Phase-3 helper to return
  `[]CorrectionEntry` sorted by `recordedAt`, each carrying
  `{correctingActivityUuid, recordedAt, recordedBy, reason}` for
  direct rendering in the UI sidebar. The scoring engine already
  reads the same `refs.correctsActivityUuid` field; no engine
  change.
- Adds a thin `POST /v1/marketing/activities/{uuid}/correct`
  handler test that asserts the corrector row appears in the
  list with the right `reason` (the handler itself was wired in
  Phase 2).

### 7.4 Verification

- `make ci-backend` clean.
- A scripted import roundtrip in the dev compose stack produces:
  - 1 engagement-CSV upload → N Activities (one per detected
    engagement cell), then re-run produces 0 new (dedup
    invariant).
  - 1 anagrafica CSV with deliberately reordered duplicate (same
    person, different email) → 1 soft-match parked review,
    resolved → 1 merged Person.
  - Mark one Activity as `corrected_by` from the Timeline UI
    (PR-5 surface, not yet) — fall back to a curl POST →
    `ListCorrectionsForActivity` returns the right entry.

---

## 8. PR-5 details — frontend + docs

**Goal**: every Phase 4 surface is reachable from the operator
console; the three Phase-3-leftover UX items also ship; docs +
memory + CLAUDE.md + OpenAPI dump are caught up.

### 8.1 New / modified React surfaces

```
frontend-admin/src/pages/marketing/
├── card-types/
│   ├── index.tsx                   # list page
│   ├── CardTypeFormModal.tsx       # create / edit
│   └── DeleteCardTypeModal.tsx     # confirms "only deletable when no cards exist"
└── persons/
    ├── PersonCardsTab.tsx          # new tab on the existing person detail
    ├── IssueCardModal.tsx          # pick type + tier + optional benefits override + expiresAt
    ├── SuspendCardModal.tsx        # reason textarea
    ├── RevokeCardModal.tsx         # reason textarea + irreversible confirmation
    └── (existing) PersonTimelineTab.tsx + CorrectActivityModal.tsx (new)
```

### 8.2 RTK Query slice extension

`frontend-admin/src/store/marketing.ts` extends the existing
slice with:

- `useListCardTypesQuery`, `useGetCardTypeQuery`,
  `useCreateCardTypeMutation`, `useUpdateCardTypeMutation`,
  `useDeleteCardTypeMutation`.
- `useListPersonCardsQuery`, `useGetCardQuery`,
  `useIssueCardMutation`, `useSuspendCardMutation`,
  `useReinstateCardMutation`, `useRevokeCardMutation`.
- `useCorrectActivityMutation` + the existing
  `useListPersonActivitiesQuery` switches to invalidating a tag
  whose key includes the corrected uuid so the strikethrough
  refresh happens in one round-trip.

### 8.3 Contact-detail Cards tab

Added between the existing Memberships and Timeline tabs in the
person-detail view. URL-synced via the existing tab pattern
(`?tab=cards`).

Layout:

- Top action button: **Issue card** (visible when
  `marketing.card.issue` is granted; otherwise hidden — the
  permission check uses the same `useRequirePermission` hook the
  Memberships tab uses).
- Table columns: Code · Type · Tier · Status · Issued at ·
  Expires at · Actions.
- Per-row actions (drop-down): Suspend / Reinstate / Revoke (the
  set is gated by the current `status` + the permission keys).

The Issue modal:

- Type selector (dropdown sourced from `useListCardTypesQuery({
  active: true })`).
- Tier selector (dropdown sourced from
  `selectedType.tiers`, hidden when the type has no tiers).
- Benefits textarea (pre-filled from
  `selectedType.defaultBenefits`, comma-separated; operator can
  edit).
- Optional `expiresAt` date picker.
- Submit → `useIssueCardMutation`; success closes the modal and
  invalidates the person-cards query tag.

Suspend / Reinstate / Revoke modals follow the same shape —
confirmation + optional reason + `useXxxCardMutation`. Revoke
specifically requires the operator to retype the card code as
an irreversibility safeguard (same friction the score-profile
delete modal already applies).

### 8.4 Card-type admin page

`/marketing/card-types` — two-pane layout matching
`/marketing/scoring`:

- Left pane: list of card types with active/inactive toggle, key,
  display name, `allowMultiplePerPerson` flag, code-format
  preview.
- Right pane: create / edit form with the fields per
  `schemas/marketing_card_types.md` — `key`, `displayName`,
  `description` (markdown), `tiers` (chip input), `codeFormat`
  (text input with live "preview generates: PREM-2026-00042"
  rendering using the parser exposed via a thin
  `GET /v1/marketing/card-types/preview-code?format=...`
  helper-route — added to PR-3 for free), `defaultBenefits`
  (chip input), `allowMultiplePerPerson` (checkbox), `active`
  (checkbox).

The preview helper-route eliminates client-side reimplementation
of the grammar — same logic, single source of truth.

### 8.5 Timeline `corrected_by` UI

The existing Timeline tab grows a per-row Correct button (visible
on activities of kinds other than `corrected_by` itself + when
the operator has `marketing.activity.write`). Clicking opens
`CorrectActivityModal` — single reason textarea + a confirmation
button that posts to
`/v1/marketing/activities/{uuid}/correct`. On success the modal
closes and the Timeline refreshes; the corrected row renders
with `text-decoration: line-through` and an inline
"↻ corrected · view note" badge. The badge hover-card or
sidebar shows the `reason` payload from the corrector row
(fetched via `useListCorrectionsForActivityQuery`).

### 8.6 Import wizard — engagement mode

The existing wizard `/marketing/imports/new` gains:

- Step 1 (adapter picker): unchanged.
- Step 2 (CSV mapping): a new "Engagement mode" checkbox below
  the column-mapping editor. Tooltip explains:
  > Treat columns named `email_opened` / `email_clicked` /
  > `email_bounced` / `email_unsubscribed` / `email_complained` /
  > `form_submitted` / `page_visited` / `event_attended` as
  > engagement events. Each truthy cell becomes an Activity with
  > the row's `occurred_at` timestamp.
- Checkbox state flips `engagementMode` on the submitted
  `ImportConfig`. Defaults to off.
- When the user uploads a CSV whose detected engagement columns
  are non-empty AND the checkbox is off, a one-line hint appears:
  "Engagement columns detected — enable Engagement mode to log
  them as Activities."

### 8.7 EN+IT i18n

Every new string added under
`frontend-admin/public/locales/{en,it}/marketing.json`. The
Italian-vs-English pattern follows the
`project_frontend_admin_i18n_phase4` memory entry — keep IT
loanwords (Tenant, Provider, Tier, Card, Suspend, Revoke) and
translate the verbs (Issue → Emetti, Reinstate → Riattiva,
Engagement mode → Modalità engagement).

### 8.8 Module CLAUDE.md + memory

- `backend/internal/addons/marketing/CLAUDE.md` — flip Phase 4
  status from "future" to "shipped", add the new collections /
  routes / permissions / scheduler description, expand the
  cross-tenant-bypass count to three.
- `~/.claude/projects/.../memory/MEMORY.md` — add a new
  `project_marketing_addon_phase4.md` entry linked from the
  Active work section. Format mirrors Phase 3's entry.
- The Phase 4 plan doc itself updates its status frontmatter
  field from `ready-to-execute` to `shipped` once PR-5 lands.

### 8.9 Verification

- `make ci-frontend-admin` clean (typecheck, eslint, tests,
  audit, build).
- Manual smoke against the dev compose stack: create a card
  type, issue a card, suspend it, reinstate it, revoke it; mark
  an activity as corrected; run an engagement-CSV import twice
  and verify the second run produces 0 new activities.
- The Phase 4 plan doc + the memory entry are part of the PR-5
  commit (same one-PR-one-doc-update pattern Phase 3 used).

---

## 9. Risks, open questions, preflight

### 9.1 Risks

- **Mongo transaction availability** — `CardService.Revoke` /
  `Expire` writes the card row + Person `$pull` + activity insert
  inside a single transaction. The dev MongoDB image runs a
  single-node replica set so transactions work, and staging /
  prod already do; double-check that no profile compose file
  omits `--replSet rs0` on the Mongo container. (Same concern
  the Phase 2 scoring path already lives with.)
- **Sequence counter contention** — `findAndModify` is atomic but
  serialises issues for a hot card type. Acceptable for typical
  ops volume (single-digit cards per second tenant-wide), but
  worth a benchmark in PR-2 if any tenant signals batch issuance.
- **Backfill of denormalised soft-match fields** — runs once at
  boot when the sentinel is unset. For a tenant with 100k Persons
  the scan is ~5–10 seconds — acceptable, but the job logs
  progress at 10% increments so operators can see it.
- **Soft-match false positives** — the index design assumes
  operators triage the queue. If a tenant sees soft-match noise
  the right knob is to disable the soft-match pass via a per-
  tenant config flag — deferred to Phase 5 if it actually
  surfaces in feedback. Phase 4 ships with soft-match always-on.

### 9.2 Preflight before PR-1

The `project_ci_release_blockers` checklist applies:

- **tenantscope analyzer** — blind to the extracted-addon module
  (`github.com/orkestra-cc/orkestra-addon-marketing`). New code
  must be clean by construction: every read via `tenantrepo.Scope`,
  every write via `tenantrepo.StampInsert*`. Manual review on
  every new repository file.
- **policycoverage analyzer** — same blind spot. Three new
  permissions land in `marketing.card_type.write` /
  `marketing.card.{issue,suspend,revoke}`; the platform
  wildcards in `platform.cedar` already cover everything
  marketing-namespaced — no `.cedar` additions needed.
- **`make openapi-dump`** — re-run after PR-3 lands the new
  routes, commit the regenerated `backend/openapi/enterprise.json`
  in the same PR. The `openapi-check` gate in CI fails the build
  on drift, so don't skip this.
- **badge-SVG EOL drift** — not relevant to Phase 4 (no test
  coverage badges change) but the pattern recurs.

### 9.3 Open questions for the user (defer answers until they
matter)

- **Per-Organization `merged` activity kind** — Phase 3 emits
  `merged` only on Person-target reviews. The schema permits
  Org-target reviews too. Adding `KindOrgMerged` (or extending
  `KindMerged`'s payload with `targetKind`) is straightforward
  but the org-card lifecycle has no analogue. Punt to Phase 5
  unless the operator console signals demand.
- **GDPR DSR cascade for cards** — when a Person is hard-deleted
  via the (future) compliance addon's right-to-be-forgotten
  pipeline, are their cards revoked-with-reason or hard-deleted?
  Mirror the Phase 2 decision on activities (hard delete with a
  separate audit-log record in the compliance addon). No code
  in Phase 4; just flag for the compliance addon's DSR design.
- **Tier-2 client surface** — the client SPA does not consume
  card data today, but the ADR-0003 audience split lets a future
  member portal read its own cards via
  `aud = "client"`. Phase 4 does not add client-aud handlers;
  when the Tier-2 client demo grows a member-portal feature,
  the handlers above are the obvious surface to mirror.

---

## 10. Deferred items (explicitly NOT in Phase 4)

- **`marketing_module_state` schema-version sentinel collection**
  — referenced by the soft-match backfill in §7.2. Land it in
  PR-4 as a single-document collection with one field
  (`softMatchBackfillRunAt`). Don't pre-emptively add more
  fields; subsequent migrations can extend it.
- **Card preview rendering as PDF** — the documents addon could
  generate printable cards via Gotenberg, but no operator has
  asked. Defer until the design doc grows a Phase 5+ section
  for it.
- **Member portal Tier-2 client surface** — see §9.3.
- **GDPR DSR cascade on cards** — see §9.3.
- **Configurable benefit catalog** — the schema's `default_benefits`
  remains a free-form `[]string` (per the schema note). A typed
  benefit catalog with eligibility rules is a Phase 5+ design.
- **Bulk card operations** — issue / suspend / revoke in batch
  (e.g. "expire all founder cards in tier silver"). Useful for
  ops, but no concrete demand and the design doc does not
  specify. Defer.
- **ESP webhook ingestion** — still Phase 5.

---

*Plan ready to execute. PR-1 starts after sign-off from the user
on the planned sub-PR split + the three Phase-3 leftovers being
in scope.*
