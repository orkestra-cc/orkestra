// Package marketing is the Orkestra marketing addon — contact base
// (Organizations, Persons, Memberships, Tags, Custom Field Schemas),
// importer pipeline, and (in future phases) immutable activity log,
// multi-profile scoring engine, and card/membership lifecycle.
//
// Phase 1 (Fondazione anagrafica MVP) ships the data layer + (in
// upcoming PRs) handlers and a CSV importer. The design lives at
// docs/plans/marketing-addon/Orkestra_marketing_addon.md and the
// per-phase implementation plan at
// docs/plans/marketing-addon/IMPLEMENTATION_PLAN.md in the orkestra
// monorepo.
package marketing

import (
	"context"
	"log/slog"
	"time"

	"github.com/danielgtaylor/huma/v2/adapters/humachi"
	"github.com/go-chi/chi/v5"
	"github.com/orkestra-cc/orkestra-addon-marketing/handlers"
	"github.com/orkestra-cc/orkestra-addon-marketing/importers"
	csvimp "github.com/orkestra-cc/orkestra-addon-marketing/importers/csv"
	excelimp "github.com/orkestra-cc/orkestra-addon-marketing/importers/excel"
	odooimp "github.com/orkestra-cc/orkestra-addon-marketing/importers/odoo"
	"github.com/orkestra-cc/orkestra-addon-marketing/jobs"
	"github.com/orkestra-cc/orkestra-addon-marketing/models"
	"github.com/orkestra-cc/orkestra-addon-marketing/repository"
	"github.com/orkestra-cc/orkestra-addon-marketing/scoring"
	"github.com/orkestra-cc/orkestra-addon-marketing/services"
	"github.com/orkestra-cc/orkestra-sdk/iface"
	"github.com/orkestra-cc/orkestra-sdk/module"
	"github.com/orkestra-cc/orkestra-sdk/modulegate"
)

// Settings is the typed view of the marketing module's config schema.
// Each field corresponds 1:1 to an entry in ConfigSchema() — keep them
// in sync. UnmarshalModule decodes the active environment into this
// struct so Init() consumes module config through a single typed
// surface (same pattern as subscriptions).
type Settings struct {
	ScoreRecomputeInterval      time.Duration `module:"scoreRecomputeInterval"`
	ScoreEagerOnInsert          bool          `module:"scoreEagerOnInsert"`
	ActivityBreakdownMax        int           `module:"activityBreakdownMax"`
	ImportWorkerConcurrency     int           `module:"importWorkerConcurrency"`
	ImportQueueBuffer           int           `module:"importQueueBuffer"`
	ImportSpoolDir              string        `module:"importSpoolDir"`
	CardExpirationCheckInterval time.Duration `module:"cardExpirationCheckInterval"`
}

// MarketingModule implements the Orkestra SDK Module interface for
// the marketing addon. Phase 1 shipped the contact base + CSV
// importer + admin UI. Phase 2 grows the module with the activity
// log, score profiles, score snapshots, and the eager + nightly
// recompute pipeline (this PR-3 wires the scheduler; PR-4 adds the
// HTTP routes; PR-5 adds the frontend).
type MarketingModule struct {
	module.BaseModule

	logger *slog.Logger

	// Handlers held on the module so RegisterRoutes can mount them
	// after Init wires the service + repo graph.
	orgHandler         *handlers.OrganizationHandler
	personHandler      *handlers.PersonHandler
	membershipHandler  *handlers.MembershipHandler
	tagHandler         *handlers.TagHandler
	customFieldHandler *handlers.CustomFieldSchemaHandler
	importsHandler     *handlers.ImportsHandler

	// Phase 2 handlers — activity log, score profile CRUD, snapshot
	// reads + per-profile leaderboard.
	activityHandler *handlers.ActivityHandler
	profileHandler  *handlers.ScoreProfileHandler
	snapshotHandler *handlers.SnapshotHandler

	// Importer registry — exposed via NewImportService when handlers are
	// wired in Init. Phase 1 ships only the CSV adapter; future adapters
	// (excel, odoo) append to the slice without changing the wiring shape.
	importerAdapters []importers.Importer

	// Phase 2 services held for downstream wiring (PR-4 handlers, the
	// recompute job) and for tests that need to inspect them.
	activitySvc  *services.ActivityService
	scoreSvc     *services.ScoreService
	profileSvc   *services.ScoreProfileService
	recomputeJob *jobs.RecomputeJob

	// recomputeCancel cancels the goroutine context created in Start.
	// Stop calls both this and recomputeJob.Stop — either path on its
	// own would exit the ticker, but belt-and-braces matches the
	// subscriptions pattern (see jobs/renewal_job.go select loop).
	recomputeCancel context.CancelFunc

	// Phase 3 — async import runner + conflict review queue + auto-emission.
	conflictReviewRepo    *repository.ConflictReviewRepository
	conflictReviewSvc     *services.ConflictReviewService
	conflictReviewHandler *handlers.ConflictReviewHandler
	importWorker          *importers.Worker
	workerCancel          context.CancelFunc
	activityEmitter       *services.ActivityEmitter

	// Phase 4 — card lifecycle: type templates, instances, sequence
	// counter for the {seq:N} placeholder, the orchestration services,
	// the expiration scheduler, and (PR-3) the HTTP handlers + the
	// three new Cedar permissions (marketing.card_type.write +
	// marketing.card.{issue,suspend,revoke}).
	cardTypeSvc        *services.CardTypeService
	cardSvc            *services.CardService
	cardExpirationJob  *jobs.CardExpirationJob
	cardExpirationStop context.CancelFunc

	cardTypeHandler *handlers.CardTypeHandler
	cardHandler     *handlers.CardHandlerWithReads
}

// NewModule returns a new instance. The registry calls this once per
// boot from cmd/server/catalog_marketing.go (and from any external
// host that wires the addon via its public mirror).
func NewModule() *MarketingModule { return &MarketingModule{} }

// Name is the stable identifier used in module_configs, route gating,
// and ORKESTRA_PROFILE addon lists. Never rename without a coordinated
// data migration.
func (m *MarketingModule) Name() string { return "marketing" }

// DisplayName is shown in the /admin/modules UI. Italian-friendly
// cognate so it sits well next to the other localized module labels
// in the operator console (e.g. "Sottoscrizioni" for subscriptions).
func (m *MarketingModule) DisplayName() string { return "Marketing" }

// Description gives the operator a one-line summary on hover/expand
// in /admin/modules.
func (m *MarketingModule) Description() string {
	return "Anagrafica contatti, importer multi-sorgente, storico attività, scoring multi-profilo e card/membership"
}

// Category marks this module as toggleable — enabled/disabled from the
// admin UI without external infra. All optional addons in orkestra
// share this category.
func (m *MarketingModule) Category() module.ModuleCategory {
	return module.CategoryToggleable
}

// Enabled returns the first-boot default state when no module_configs
// document exists yet. False keeps marketing opt-in everywhere except
// the enterprise SKU, where the "*" entry in profileAddons
// (pkg/sdk/module/config_service.go) pre-enables every optional
// addon on first boot.
func (m *MarketingModule) Enabled() bool { return false }

// Dependencies is empty in Phase 1 — the contact base does not consume
// any other module. Phase 2 (scoring) and beyond may add optional deps
// (e.g. notification for campaign delivery) via ServiceRegistry
// lookups rather than hard Dependencies entries.
func (m *MarketingModule) Dependencies() []string { return nil }

// Collections returns the MongoDB collections this module owns,
// declaring the indexes the registry creates at boot. Index
// declarations follow the schemas at
// docs/plans/marketing-addon/schemas/ — adjust both together when
// fields evolve.
//
// Limitation: the SDK's IndexSpec does not expose
// PartialFilterExpression, so the schema's "unique partial" entries
// on marketing_memberships (one Active=true per (person,org) pair,
// one Active+Primary=true per person) cannot be enforced at the
// index level today. Service-layer helpers in
// repository/membership_repo.go (UnsetPrimaryForPerson, Close)
// preserve the invariant under the typical mutation flows; widening
// the SDK to support partial filters is tracked as a Phase-2+
// follow-up.
func (m *MarketingModule) Collections() []module.CollectionSpec {
	return []module.CollectionSpec{
		{Name: models.OrganizationsCollection, Indexes: []module.IndexSpec{
			{Keys: map[string]int{"uuid": 1}, Unique: true},
			{OrderedKeys: []module.IndexKey{
				{Field: "tenantId", Direction: 1},
				{Field: "vat", Direction: 1},
			}, Unique: true, Sparse: true},
			{OrderedKeys: []module.IndexKey{
				{Field: "tenantId", Direction: 1},
				{Field: "taxCode", Direction: 1},
			}, Unique: true, Sparse: true},
			{OrderedKeys: []module.IndexKey{
				{Field: "tenantId", Direction: 1},
				{Field: "kind", Direction: 1},
			}},
			{OrderedKeys: []module.IndexKey{
				{Field: "tenantId", Direction: 1},
				{Field: "tags", Direction: 1},
			}},
			// Phase 4 (PR-4 soft-match leftover) — backs
			// OrganizationRepository.FindSoftMatchByLegalName. Sparse
			// so legacy rows without the denorm don't perturb the
			// index size; the unique constraint is intentionally not
			// applied because two different legal entities can share a
			// canonicalized name (e.g. "ACME SRL" in two regions).
			{OrderedKeys: []module.IndexKey{
				{Field: "tenantId", Direction: 1},
				{Field: "legalNameNormalized", Direction: 1},
			}, Sparse: true},
			{OrderedKeys: []module.IndexKey{
				{Field: "tenantId", Direction: 1},
				{Field: "updatedAt", Direction: -1},
			}},
		}},
		{Name: models.PersonsCollection, Indexes: []module.IndexSpec{
			{Keys: map[string]int{"uuid": 1}, Unique: true},
			// Multikey unique sparse on emails.address — every email
			// is unique per tenant regardless of which entry carries
			// it. Sparse so persons without emails don't conflict.
			{OrderedKeys: []module.IndexKey{
				{Field: "tenantId", Direction: 1},
				{Field: "emails.address", Direction: 1},
			}, Unique: true, Sparse: true},
			{OrderedKeys: []module.IndexKey{
				{Field: "tenantId", Direction: 1},
				{Field: "tags", Direction: 1},
			}},
			// Index lands now even though Phase 4 populates the
			// field — avoids a write-blocking index build later.
			{OrderedKeys: []module.IndexKey{
				{Field: "tenantId", Direction: 1},
				{Field: "activeCardUuids", Direction: 1},
			}, Sparse: true},
			// Phase 4 (PR-4 soft-match leftover) — back the
			// PersonRepository.FindSoftMatchByNameAndPhone path. The
			// (firstNameLower, lastNameLower) compound carries the
			// names; the multikey phoneLast10 narrows to candidates
			// sharing a normalized number. Both are sparse so legacy
			// rows without the denorm don't bloat the index.
			{OrderedKeys: []module.IndexKey{
				{Field: "tenantId", Direction: 1},
				{Field: "firstNameLower", Direction: 1},
				{Field: "lastNameLower", Direction: 1},
			}, Sparse: true},
			{OrderedKeys: []module.IndexKey{
				{Field: "tenantId", Direction: 1},
				{Field: "phoneLast10", Direction: 1},
			}, Sparse: true},
			{OrderedKeys: []module.IndexKey{
				{Field: "tenantId", Direction: 1},
				{Field: "updatedAt", Direction: -1},
			}},
		}},
		{Name: models.MembershipsCollection, Indexes: []module.IndexSpec{
			{Keys: map[string]int{"uuid": 1}, Unique: true},
			{OrderedKeys: []module.IndexKey{
				{Field: "tenantId", Direction: 1},
				{Field: "personUuid", Direction: 1},
			}},
			{OrderedKeys: []module.IndexKey{
				{Field: "tenantId", Direction: 1},
				{Field: "orgUuid", Direction: 1},
			}},
			{OrderedKeys: []module.IndexKey{
				{Field: "tenantId", Direction: 1},
				{Field: "personUuid", Direction: 1},
				{Field: "orgUuid", Direction: 1},
			}},
			{OrderedKeys: []module.IndexKey{
				{Field: "tenantId", Direction: 1},
				{Field: "personUuid", Direction: 1},
				{Field: "primary", Direction: 1},
			}},
			{OrderedKeys: []module.IndexKey{
				{Field: "tenantId", Direction: 1},
				{Field: "active", Direction: 1},
			}},
		}},
		{Name: models.TagsCollection, Indexes: []module.IndexSpec{
			{Keys: map[string]int{"uuid": 1}, Unique: true},
			{OrderedKeys: []module.IndexKey{
				{Field: "tenantId", Direction: 1},
				{Field: "slug", Direction: 1},
			}, Unique: true},
			{OrderedKeys: []module.IndexKey{
				{Field: "tenantId", Direction: 1},
				{Field: "parentUuid", Direction: 1},
			}},
			{OrderedKeys: []module.IndexKey{
				{Field: "tenantId", Direction: 1},
				{Field: "path", Direction: 1},
			}},
		}},
		{Name: models.CustomFieldSchemasCollection, Indexes: []module.IndexSpec{
			{Keys: map[string]int{"uuid": 1}, Unique: true},
			{OrderedKeys: []module.IndexKey{
				{Field: "tenantId", Direction: 1},
				{Field: "targetCollection", Direction: 1},
			}, Unique: true},
		}},
		{Name: models.ImportJobsCollection, Indexes: []module.IndexSpec{
			{Keys: map[string]int{"uuid": 1}, Unique: true},
			{OrderedKeys: []module.IndexKey{
				{Field: "tenantId", Direction: 1},
				{Field: "createdAt", Direction: -1},
			}},
			{OrderedKeys: []module.IndexKey{
				{Field: "tenantId", Direction: 1},
				{Field: "status", Direction: 1},
			}},
			// Phase 3 — idempotency lookup. Sparse because Phase 1 rows
			// predate the field.
			{OrderedKeys: []module.IndexKey{
				{Field: "tenantId", Direction: 1},
				{Field: "idempotencyKey", Direction: 1},
				{Field: "createdAt", Direction: -1},
			}, Sparse: true},
		}},
		// Phase 2 — Storicizzazione & scoring.
		//
		// marketing_activities: append-only event log. Indexes optimise
		// for the timeline read (per-person + per-org chronological),
		// the score-engine read (per-kind chronological), the dedup
		// invariant, the per-ref analytics queries (campaign / event /
		// card), and the per-source audit query.
		{Name: models.ActivitiesCollection, Indexes: []module.IndexSpec{
			{Keys: map[string]int{"uuid": 1}, Unique: true},
			{OrderedKeys: []module.IndexKey{
				{Field: "tenantId", Direction: 1},
				{Field: "personUuid", Direction: 1},
				{Field: "occurredAt", Direction: -1},
			}},
			{OrderedKeys: []module.IndexKey{
				{Field: "tenantId", Direction: 1},
				{Field: "orgUuid", Direction: 1},
				{Field: "occurredAt", Direction: -1},
			}, Sparse: true},
			{OrderedKeys: []module.IndexKey{
				{Field: "tenantId", Direction: 1},
				{Field: "kind", Direction: 1},
				{Field: "occurredAt", Direction: -1},
			}},
			// dedupKey is unique across the whole collection (the
			// hash already incorporates personUuid). The Phase 2
			// plan §2.2 calls this out as the idempotence gate.
			{Keys: map[string]int{"dedupKey": 1}, Unique: true},
			{OrderedKeys: []module.IndexKey{
				{Field: "tenantId", Direction: 1},
				{Field: "refs.campaignUuid", Direction: 1},
			}, Sparse: true},
			{OrderedKeys: []module.IndexKey{
				{Field: "tenantId", Direction: 1},
				{Field: "refs.eventUuid", Direction: 1},
			}, Sparse: true},
			{OrderedKeys: []module.IndexKey{
				{Field: "tenantId", Direction: 1},
				{Field: "refs.cardUuid", Direction: 1},
			}, Sparse: true},
			{OrderedKeys: []module.IndexKey{
				{Field: "tenantId", Direction: 1},
				{Field: "source", Direction: 1},
				{Field: "recordedAt", Direction: -1},
			}},
		}},
		// marketing_score_profiles: small collection (1-10 rows per
		// tenant), reads dominated by the admin UI + the score
		// engine's per-tick active-profile fetch.
		{Name: models.ScoreProfilesCollection, Indexes: []module.IndexSpec{
			{Keys: map[string]int{"uuid": 1}, Unique: true},
			{OrderedKeys: []module.IndexKey{
				{Field: "tenantId", Direction: 1},
				{Field: "name", Direction: 1},
			}, Unique: true},
			{OrderedKeys: []module.IndexKey{
				{Field: "tenantId", Direction: 1},
				{Field: "active", Direction: 1},
			}},
		}},
		// marketing_score_snapshots: cache rebuildable from the
		// activity log. The (tenant, person, profile) unique index is
		// the upsert key — concurrent eager + nightly recomputers
		// converge deterministically on the same document.
		{Name: models.ScoreSnapshotsCollection, Indexes: []module.IndexSpec{
			{Keys: map[string]int{"uuid": 1}, Unique: true},
			{OrderedKeys: []module.IndexKey{
				{Field: "tenantId", Direction: 1},
				{Field: "personUuid", Direction: 1},
				{Field: "profileUuid", Direction: 1},
			}, Unique: true},
			{OrderedKeys: []module.IndexKey{
				{Field: "tenantId", Direction: 1},
				{Field: "profileUuid", Direction: 1},
				{Field: "value", Direction: -1},
			}},
			{OrderedKeys: []module.IndexKey{
				{Field: "tenantId", Direction: 1},
				{Field: "profileUuid", Direction: 1},
				{Field: "stale", Direction: 1},
			}},
			{OrderedKeys: []module.IndexKey{
				{Field: "tenantId", Direction: 1},
				{Field: "personUuid", Direction: 1},
			}},
		}},
		// Phase 3 — Import avanzati.
		//
		// marketing_conflict_reviews: queue of import-flagged dedup
		// conflicts awaiting operator resolution. The (tenant, status)
		// index drives the pending queue read; the per-job index drives
		// the "show me every review from this import" view that the
		// imports list deep-links to.
		{Name: models.ConflictReviewsCollection, Indexes: []module.IndexSpec{
			{Keys: map[string]int{"uuid": 1}, Unique: true},
			{OrderedKeys: []module.IndexKey{
				{Field: "tenantId", Direction: 1},
				{Field: "status", Direction: 1},
			}},
			{OrderedKeys: []module.IndexKey{
				{Field: "tenantId", Direction: 1},
				{Field: "importJobUuid", Direction: 1},
			}},
			{OrderedKeys: []module.IndexKey{
				{Field: "tenantId", Direction: 1},
				{Field: "targetKind", Direction: 1},
				{Field: "status", Direction: 1},
			}},
			{OrderedKeys: []module.IndexKey{
				{Field: "tenantId", Direction: 1},
				{Field: "existingUuid", Direction: 1},
			}},
		}},
		// Phase 4 — Card lifecycle.
		//
		// marketing_card_types: per-tenant card templates. (tenant, key)
		// is the operator-facing slug uniqueness; (tenant, active) drives
		// the "list active types" admin view.
		{Name: models.CardTypesCollection, Indexes: []module.IndexSpec{
			{Keys: map[string]int{"uuid": 1}, Unique: true},
			{OrderedKeys: []module.IndexKey{
				{Field: "tenantId", Direction: 1},
				{Field: "key", Direction: 1},
			}, Unique: true},
			{OrderedKeys: []module.IndexKey{
				{Field: "tenantId", Direction: 1},
				{Field: "active", Direction: 1},
			}},
			{OrderedKeys: []module.IndexKey{
				{Field: "tenantId", Direction: 1},
				{Field: "updatedAt", Direction: -1},
			}},
		}},
		// marketing_cards: card instances emitted to a Person. The
		// (tenant, code) unique index is the fail-safe against
		// code-collision races; (tenant, personUuid) drives the
		// contact-detail Cards tab; (tenant, status, expiresAt) is the
		// expiration scheduler's drain query. The
		// "one active per (person, type)" constraint cannot be expressed
		// as a partial unique index on the current SDK IndexSpec — it
		// is enforced at the service layer via a pre-write probe.
		{Name: models.CardsCollection, Indexes: []module.IndexSpec{
			{Keys: map[string]int{"uuid": 1}, Unique: true},
			{OrderedKeys: []module.IndexKey{
				{Field: "tenantId", Direction: 1},
				{Field: "code", Direction: 1},
			}, Unique: true},
			{OrderedKeys: []module.IndexKey{
				{Field: "tenantId", Direction: 1},
				{Field: "personUuid", Direction: 1},
			}},
			{OrderedKeys: []module.IndexKey{
				{Field: "tenantId", Direction: 1},
				{Field: "cardTypeUuid", Direction: 1},
				{Field: "status", Direction: 1},
			}},
			{OrderedKeys: []module.IndexKey{
				{Field: "tenantId", Direction: 1},
				{Field: "tier", Direction: 1},
				{Field: "status", Direction: 1},
			}, Sparse: true},
			{OrderedKeys: []module.IndexKey{
				{Field: "tenantId", Direction: 1},
				{Field: "status", Direction: 1},
				{Field: "expiresAt", Direction: 1},
			}, Sparse: true},
			{OrderedKeys: []module.IndexKey{
				{Field: "tenantId", Direction: 1},
				{Field: "issuedAt", Direction: -1},
			}},
		}},
		// marketing_card_sequences: internal per-(tenant, cardType)
		// counter backing the {seq:N} placeholder. One row per pair;
		// findAndModify($inc, upsert) keeps the value monotonic and
		// race-free.
		{Name: models.CardSequencesCollection, Indexes: []module.IndexSpec{
			{OrderedKeys: []module.IndexKey{
				{Field: "tenantId", Direction: 1},
				{Field: "cardTypeUuid", Direction: 1},
			}, Unique: true},
		}},
	}
}

// NavItems contributes the marketing surface to the operator sidebar.
// The realm "business" lines up with sales/subscriptions/billing —
// every operator running Tier-2 client revenue work sees these next
// to the related selling surfaces. Tier "internal" hides the menu
// from external (Tier-2) tenants.
//
// Paths line up with the React route declarations in
// frontend-admin/src/modules/marketing.tsx (the routes' module manifest).
func (m *MarketingModule) NavItems() []module.NavItemSpec {
	return []module.NavItemSpec{{
		Realm: "business", Tier: "internal",
		Name: "Marketing", Icon: "users", Path: "/marketing/contacts", Active: true,
		Children: []module.NavItemSpec{
			{Name: "Contacts", Icon: "address-book", Path: "/marketing/contacts", Active: true},
			{Name: "Tags", Icon: "tags", Path: "/marketing/tags", Active: true},
			{Name: "Custom Fields", Icon: "list-check", Path: "/marketing/custom-fields", Active: true},
			{Name: "Imports", Icon: "file-import", Path: "/marketing/imports", Active: true},
			{Name: "Reviews", Icon: "balance-scale", Path: "/marketing/reviews", Active: true},
			{Name: "Scoring", Icon: "chart-line", Path: "/marketing/scoring", Active: true},
		},
	}}
}

// ConfigSchema declares the per-tenant runtime knobs operators can
// edit at /admin/modules. Phase 2 introduces three keys — all
// scoring-engine tuning. EnvVars seed the first-boot defaults on a
// fresh install (see backend/CLAUDE.md "first boot of a brand-new
// install").
//
// scoreEagerOnInsert flips eager recomputes off for import bursts —
// when set false the nightly job is the only recompute path. The
// flag is read once at Init time; flipping it requires a module
// disable+enable cycle to take effect (acceptable: the use case is
// "I'm about to run a 100k-row import, turn eager off for an hour
// then back on").
func (m *MarketingModule) ConfigSchema() []module.ConfigField {
	return []module.ConfigField{
		{Key: "scoreRecomputeInterval", Label: "Score nightly-recompute interval", Type: module.FieldDuration, Default: "24h", EnvVar: "MARKETING_SCORE_RECOMPUTE_INTERVAL"},
		{Key: "scoreEagerOnInsert", Label: "Recompute score on activity insert", Type: module.FieldBool, Default: "true", EnvVar: "MARKETING_SCORE_EAGER_ON_INSERT"},
		{Key: "activityBreakdownMax", Label: "Max breakdown entries per snapshot", Type: module.FieldInt, Default: "100", EnvVar: "MARKETING_ACTIVITY_BREAKDOWN_MAX"},
		// Phase 3 — async import runner.
		{Key: "importWorkerConcurrency", Label: "Import worker concurrency", Type: module.FieldInt, Default: "2", EnvVar: "MARKETING_IMPORT_WORKER_CONCURRENCY"},
		{Key: "importQueueBuffer", Label: "Import queue buffer", Type: module.FieldInt, Default: "32", EnvVar: "MARKETING_IMPORT_QUEUE_BUFFER"},
		{Key: "importSpoolDir", Label: "Import spool directory", Type: module.FieldString, Default: "/var/lib/orkestra/marketing/spool", EnvVar: "MARKETING_IMPORT_SPOOL_DIR"},
		// Phase 4 — card lifecycle.
		{Key: "cardExpirationCheckInterval", Label: "Card expiration scanner interval", Type: module.FieldDuration, Default: "1h", EnvVar: "MARKETING_CARD_EXPIRATION_INTERVAL"},
	}
}

// Permissions declares the Cedar permission catalog this module
// publishes.
//
// Phase 1 shipped the contact-base bucket (read/write/delete) and
// the import bucket. Phase 2 PR-3 added marketing.score_profile.write
// for the scoring admin surface; Phase 2 PR-4 (this) adds
// marketing.activity.write for manual activity logging + corrections.
//
// Read access for activities + snapshots + score profiles folds into
// marketing.contact.read at the handler boundary (Phase 2 plan §2.3):
// an operator who can see the contact base must also see the
// timeline + scoring that contextualises it. Granting contact-read
// while denying activity-read would be semantically incoherent.
//
// Later phases add:
//   - marketing.card_type.write / marketing.card.{issue,suspend,revoke}
//     (Phase 4, card lifecycle)
func (m *MarketingModule) Permissions() []iface.PermissionSpec {
	return []iface.PermissionSpec{
		{Key: "marketing.contact.read", Module: m.Name(), Description: "View persons, organizations, memberships, tags, custom-field schemas, import-job audit, activity timelines, score profiles, snapshots, and conflict-review queue"},
		{Key: "marketing.contact.write", Module: m.Name(), Description: "Create and update persons, organizations, memberships, tags, and custom-field schemas"},
		{Key: "marketing.contact.delete", Module: m.Name(), Description: "Hard-delete contacts (org/person cascades to memberships) and tags/schemas"},
		{Key: "marketing.import.run", Module: m.Name(), Description: "Trigger CSV/Excel/Odoo imports of contact data (separate gate from contact.write so import access can be granted independently)"},
		{Key: "marketing.activity.write", Module: m.Name(), Description: "Log manual activities (call, meeting, note, correction) on the contact timeline"},
		{Key: "marketing.score_profile.write", Module: m.Name(), Description: "Create and update scoring profiles (rules, decay, filters); each save bumps the profile version and invalidates downstream snapshots"},
		{Key: "marketing.conflict.resolve", Module: m.Name(), Description: "Resolve marketing import conflict reviews (keep_existing / take_incoming / manual_merge / dismiss)"},
		// Phase 4 — card lifecycle. Reads on card types + cards fold
		// into marketing.contact.read (consistent with the activities
		// and conflict-reviews fold-in). Writes split across three
		// permissions so role catalogues can match real-world
		// separation of duties: ops emit cards more often than they
		// retire types, and revocation is irreversible so its bucket
		// is intentionally narrower than suspend.
		{Key: "marketing.card_type.write", Module: m.Name(), Description: "Create, update, and delete marketing card types"},
		{Key: "marketing.card.issue", Module: m.Name(), Description: "Issue a marketing card to a person"},
		{Key: "marketing.card.suspend", Module: m.Name(), Description: "Suspend or reinstate an active marketing card"},
		{Key: "marketing.card.revoke", Module: m.Name(), Description: "Revoke a marketing card (terminal, irreversible)"},
	}
}

// Init wires the data + service + handler + job graph. The registry
// calls this after every declared dependency has finished its own
// Init.
//
// Wiring order:
//
//  1. Module config decoded into Settings (one typed surface).
//  2. Repositories (Mongo collections only, no business logic).
//  3. Phase 1 services + handlers (contacts, tags, importer).
//  4. Phase 2 scoring engine (pure, no I/O).
//  5. Phase 2 services — ActivityService → ScoreService →
//     ScoreProfileService. The arrow is the dependency order:
//     ScoreService is registered as an ActivityService listener so
//     activity inserts trigger eager recomputes; ScoreProfileService
//     calls ScoreService.InvalidateProfile on every save.
//  6. RecomputeJob, held on the module for Start/Stop.
//
// The order matters because RegisterListener must run after both
// the ActivityService (producer) and the ScoreService (consumer)
// exist.
func (m *MarketingModule) Init(deps *module.Dependencies) error {
	m.logger = deps.Logger

	var settings Settings
	if err := deps.ConfigService.UnmarshalModule(context.Background(), m.Name(), &settings); err != nil {
		return err
	}
	if settings.ScoreRecomputeInterval <= 0 {
		settings.ScoreRecomputeInterval = 24 * time.Hour
	}
	if settings.ActivityBreakdownMax <= 0 {
		settings.ActivityBreakdownMax = scoring.DefaultBreakdownMax
	}
	if settings.ImportWorkerConcurrency <= 0 {
		settings.ImportWorkerConcurrency = 2
	}
	if settings.ImportQueueBuffer <= 0 {
		settings.ImportQueueBuffer = 32
	}
	if settings.ImportSpoolDir == "" {
		settings.ImportSpoolDir = "/var/lib/orkestra/marketing/spool"
	}
	if settings.CardExpirationCheckInterval <= 0 {
		settings.CardExpirationCheckInterval = time.Hour
	}

	// --- Phase 1 wiring ---
	orgRepo := repository.NewOrganizationRepository(deps.DB)
	personRepo := repository.NewPersonRepository(deps.DB)
	mshipRepo := repository.NewMembershipRepository(deps.DB)
	tagRepo := repository.NewTagRepository(deps.DB)
	cfSchemaRepo := repository.NewCustomFieldSchemaRepository(deps.DB)
	jobRepo := repository.NewImportJobRepository(deps.DB)

	cfSvc := services.NewCustomFieldService(cfSchemaRepo)
	orgSvc := services.NewOrganizationService(orgRepo, cfSvc, mshipRepo)
	personSvc := services.NewPersonService(personRepo, cfSvc, mshipRepo)
	mshipSvc := services.NewMembershipService(mshipRepo)
	tagSvc := services.NewTagService(tagRepo)

	m.importerAdapters = []importers.Importer{csvimp.New(), excelimp.New(), odooimp.New()}

	// --- Phase 3 wiring — async import worker + conflict review repo ---
	m.conflictReviewRepo = repository.NewConflictReviewRepository(deps.DB)

	workerCatalog := make(map[string]importers.Importer, len(m.importerAdapters))
	for _, a := range m.importerAdapters {
		workerCatalog[a.Name()] = a
	}
	m.importWorker = importers.NewWorker(importers.WorkerDeps{
		Logger:      deps.Logger,
		JobRepo:     jobRepo,
		OrgRepo:     orgRepo,
		PersonRepo:  personRepo,
		MshipRepo:   mshipRepo,
		TagRepo:     tagRepo,
		Catalog:     workerCatalog,
		SpoolDir:    settings.ImportSpoolDir,
		Concurrency: settings.ImportWorkerConcurrency,
		QueueBuffer: settings.ImportQueueBuffer,
	})

	importSvc := services.NewImportService(jobRepo, m.importWorker, m.importerAdapters...)

	m.orgHandler = handlers.NewOrganizationHandler(orgSvc)
	m.personHandler = handlers.NewPersonHandler(personSvc)
	m.membershipHandler = handlers.NewMembershipHandler(mshipSvc)
	m.tagHandler = handlers.NewTagHandler(tagSvc)
	m.customFieldHandler = handlers.NewCustomFieldSchemaHandler(cfSvc)
	m.importsHandler = handlers.NewImportsHandler(importSvc)

	// --- Phase 2 wiring ---
	actRepo := repository.NewActivityRepository(deps.DB)
	profRepo := repository.NewScoreProfileRepository(deps.DB)
	snapRepo := repository.NewScoreSnapshotRepository(deps.DB)

	engine := scoring.NewEngine(settings.ActivityBreakdownMax, deps.Logger)

	m.scoreSvc = services.NewScoreService(snapRepo, profRepo, actRepo, personRepo, engine, settings.ScoreEagerOnInsert, deps.Logger)
	m.activitySvc = services.NewActivityService(actRepo, mshipRepo, deps.Logger)
	m.profileSvc = services.NewScoreProfileService(profRepo, m.scoreSvc, deps.Logger)

	// Register eager-recompute hook. The closure captures
	// m.scoreSvc, so the listener slice on ActivityService is the
	// only reference path between activity inserts and score
	// recomputation — no global registry, no service-locator
	// indirection.
	m.activitySvc.RegisterListener(m.scoreSvc.OnActivityInserted)

	// Phase 3 — auto-emission. ActivityEmitter wraps ActivityService
	// for the system-source emissions (tag_added/_removed via the
	// PersonService update listener, imported direct from the importer
	// pipeline, merged direct from ConflictReviewService.Resolve).
	m.activityEmitter = services.NewActivityEmitter(m.activitySvc, deps.Logger)
	personSvc.RegisterUpdateListener(m.activityEmitter.OnPersonUpdated)

	// Phase 3 — conflict review queue.
	m.conflictReviewSvc = services.NewConflictReviewService(
		m.conflictReviewRepo,
		personRepo,
		orgRepo,
		jobRepo,
		m.activitySvc,
		m.activityEmitter,
		deps.Logger,
	)
	m.conflictReviewHandler = handlers.NewConflictReviewHandler(m.conflictReviewSvc)

	// Late-wire the worker's optional collaborators now that the
	// ConflictReviewService + ActivityEmitter exist. The worker was
	// constructed earlier (before the services that depend on the
	// pipeline-adjacent repos) so the late hook is the simplest way
	// to break the build order without forcing two-phase Init.
	m.importWorker.WireServices(m.conflictReviewSvc, m.activityEmitter)

	m.recomputeJob = jobs.NewRecomputeJob(m.scoreSvc, settings.ScoreRecomputeInterval, deps.Logger)

	m.activityHandler = handlers.NewActivityHandler(m.activitySvc)
	m.profileHandler = handlers.NewScoreProfileHandler(m.profileSvc, snapRepo)
	m.snapshotHandler = handlers.NewSnapshotHandler(snapRepo)

	// --- Phase 4 wiring — card lifecycle ---
	//
	// CardTypeService caches the CodeFormat AST in-process; CardService
	// is the single funnel for the §7 state machine and Activity emit.
	// The expiration job runs as a Start/Stop-gated goroutine via
	// CardExpirationJob.
	cardTypeRepo := repository.NewCardTypeRepository(deps.DB)
	cardRepo := repository.NewCardRepository(deps.DB)
	cardSeqRepo := repository.NewCardSequenceRepository(deps.DB)

	m.cardTypeSvc = services.NewCardTypeService(cardTypeRepo, cardRepo)
	m.cardSvc = services.NewCardService(services.CardServiceDeps{
		TypeService: m.cardTypeSvc,
		CardRepo:    cardRepo,
		SeqRepo:     cardSeqRepo,
		PersonRepo:  personRepo,
		ActivitySvc: m.activitySvc,
		Logger:      deps.Logger,
	})
	m.cardExpirationJob = jobs.NewCardExpirationJob(cardRepo, m.cardSvc, settings.CardExpirationCheckInterval, deps.Logger)

	m.cardTypeHandler = handlers.NewCardTypeHandler(m.cardTypeSvc)
	m.cardHandler = handlers.NewCardHandlerWithReads(m.cardSvc, cardRepo)

	// Wire the persons-list `?activeCardOfType=` resolver — the
	// PersonHandler needs the CardRepository read-path to translate
	// a type uuid into the set of currently-active card uuids.
	if m.personHandler != nil {
		m.personHandler.WithCardRepo(cardRepo)
	}

	m.logger.Info("Marketing module initialized",
		slog.Duration("scoreRecomputeInterval", settings.ScoreRecomputeInterval),
		slog.Bool("scoreEagerOnInsert", settings.ScoreEagerOnInsert),
		slog.Int("activityBreakdownMax", settings.ActivityBreakdownMax),
		slog.Int("importWorkerConcurrency", settings.ImportWorkerConcurrency),
		slog.Int("importQueueBuffer", settings.ImportQueueBuffer),
		slog.String("importSpoolDir", settings.ImportSpoolDir),
		slog.Duration("cardExpirationCheckInterval", settings.CardExpirationCheckInterval),
	)
	return nil
}

// Start spawns the nightly recompute job + the import worker pool.
// The registry calls Start only when the module is enabled —
// disabled modules go through Init (so collections + handlers are
// wired and the gated routes return 503) but skip Start, so the
// background goroutines don't run.
func (m *MarketingModule) Start(_ context.Context) error {
	if m.recomputeJob != nil {
		jobCtx, cancel := context.WithCancel(context.Background())
		m.recomputeCancel = cancel
		go m.recomputeJob.Start(jobCtx)
	}
	if m.importWorker != nil {
		workerCtx, cancel := context.WithCancel(context.Background())
		m.workerCancel = cancel
		if err := m.importWorker.Start(workerCtx); err != nil {
			m.logger.Error("Marketing import worker boot recovery failed", slog.String("err", err.Error()))
		}
	}
	if m.cardExpirationJob != nil {
		expCtx, cancel := context.WithCancel(context.Background())
		m.cardExpirationStop = cancel
		go m.cardExpirationJob.Start(expCtx)
	}
	return nil
}

// Stop cancels the ticker + signals the worker to drain. Both signals
// (ctx cancel + queue close in worker.Stop) reach the goroutines'
// select loops; whichever fires first wins.
func (m *MarketingModule) Stop(ctx context.Context) error {
	if m.recomputeCancel != nil {
		m.recomputeCancel()
	}
	if m.recomputeJob != nil {
		m.recomputeJob.Stop()
	}
	if m.workerCancel != nil {
		m.workerCancel()
	}
	if m.importWorker != nil {
		if err := m.importWorker.Stop(ctx); err != nil {
			m.logger.Warn("Marketing import worker shutdown timed out", slog.String("err", err.Error()))
		}
	}
	if m.cardExpirationStop != nil {
		m.cardExpirationStop()
	}
	if m.cardExpirationJob != nil {
		m.cardExpirationJob.Stop()
	}
	return nil
}

// HealthCheck reports the module as healthy as long as Init has run.
// A future enhancement could probe the recompute job's last-tick
// timestamp; today the per-module health surface doesn't require
// background-job liveness.
func (m *MarketingModule) HealthCheck(_ context.Context) error { return nil }

// RegisterRoutes mounts the CRUD surface on the operator API. The
// addon serves Tier-1 internal-tenant operators only — Tier-2 client
// surfaces will arrive in Phase 5 (campaigns, segments, preference
// center) if the design requires them.
//
// Three permission buckets, mirrored to read / write / delete:
//   - marketing.contact.read   → list + get on all 5 collections
//   - marketing.contact.write  → create + update on all 5 collections
//   - marketing.contact.delete → delete on all 5 collections (cascades
//     on org/person via the service layer)
//
// Every bucket is wrapped in ModuleGate so disabled-module routes
// return 503 (the registry does NOT detach the routes at runtime),
// and in RequireInternalTenant so only operators can hit the routes
// — these are Tier-1 management endpoints, not Tier-2 self-service.
func (m *MarketingModule) RegisterRoutes(ri *module.RouteInfo) {
	if m.orgHandler == nil {
		return
	}
	ri.Operator.ProtectedRouter.Group(func(gated chi.Router) {
		gated.Use(modulegate.ModuleGate(ri.ConfigService, m.Name()))

		// READ bucket — includes the import-job audit read surface, the
		// Phase 2 activity timeline, the score-profile catalog +
		// leaderboard, and snapshot reads. Phase 2 plan §2.3 folds
		// activity-read / score-profile-read into contact.read because
		// granting one without the other is incoherent.
		gated.Group(func(r chi.Router) {
			r.Use(ri.Operator.AuthMW.RequireInternalTenant())
			r.Use(ri.Operator.AuthMW.RequirePermission("marketing.contact.read"))
			api := humachi.New(r, ri.APIConfig)
			handlers.RegisterOrgReadRoutes(api, m.orgHandler)
			handlers.RegisterPersonReadRoutes(api, m.personHandler)
			handlers.RegisterMembershipReadRoutes(api, m.membershipHandler)
			handlers.RegisterTagReadRoutes(api, m.tagHandler)
			handlers.RegisterCustomFieldReadRoutes(api, m.customFieldHandler)
			handlers.RegisterImportReadRoutes(api, m.importsHandler)
			handlers.RegisterActivityReadRoutes(api, m.activityHandler)
			handlers.RegisterScoreProfileReadRoutes(api, m.profileHandler)
			handlers.RegisterSnapshotReadRoutes(api, m.snapshotHandler)
			handlers.RegisterConflictReviewReadRoutes(api, m.conflictReviewHandler)
			handlers.RegisterCardTypeReadRoutes(api, m.cardTypeHandler)
			handlers.RegisterCardReadRoutes(api, m.cardHandler)
		})

		// IMPORT bucket — separate gate so import access can be
		// granted independently of contact-write. Operators with
		// the run grant can trigger a sync import; the underlying
		// writes happen with service credentials, not the caller's
		// permission, so the bucket does not also need write.
		gated.Group(func(r chi.Router) {
			r.Use(ri.Operator.AuthMW.RequireInternalTenant())
			r.Use(ri.Operator.AuthMW.RequirePermission("marketing.import.run"))
			api := humachi.New(r, ri.APIConfig)
			handlers.RegisterImportRunRoutes(api, m.importsHandler)
		})

		// WRITE bucket
		gated.Group(func(r chi.Router) {
			r.Use(ri.Operator.AuthMW.RequireInternalTenant())
			r.Use(ri.Operator.AuthMW.RequirePermission("marketing.contact.write"))
			api := humachi.New(r, ri.APIConfig)
			handlers.RegisterOrgWriteRoutes(api, m.orgHandler)
			handlers.RegisterPersonWriteRoutes(api, m.personHandler)
			handlers.RegisterMembershipWriteRoutes(api, m.membershipHandler)
			handlers.RegisterTagWriteRoutes(api, m.tagHandler)
			handlers.RegisterCustomFieldWriteRoutes(api, m.customFieldHandler)
		})

		// DELETE bucket
		gated.Group(func(r chi.Router) {
			r.Use(ri.Operator.AuthMW.RequireInternalTenant())
			r.Use(ri.Operator.AuthMW.RequirePermission("marketing.contact.delete"))
			api := humachi.New(r, ri.APIConfig)
			handlers.RegisterOrgDeleteRoutes(api, m.orgHandler)
			handlers.RegisterPersonDeleteRoutes(api, m.personHandler)
			handlers.RegisterMembershipDeleteRoutes(api, m.membershipHandler)
			handlers.RegisterTagDeleteRoutes(api, m.tagHandler)
			handlers.RegisterCustomFieldDeleteRoutes(api, m.customFieldHandler)
		})

		// ACTIVITY WRITE bucket — manual activity logging + correction.
		// Separate gate from contact.write because logging real-world
		// touchpoints is a different authority than editing the contact
		// record itself.
		gated.Group(func(r chi.Router) {
			r.Use(ri.Operator.AuthMW.RequireInternalTenant())
			r.Use(ri.Operator.AuthMW.RequirePermission("marketing.activity.write"))
			api := humachi.New(r, ri.APIConfig)
			handlers.RegisterActivityWriteRoutes(api, m.activityHandler)
		})

		// SCORE PROFILE WRITE bucket — profile CRUD. Save bumps version
		// and bulk-marks downstream snapshots as stale; the recompute
		// job + the next eager hit on each person settle the new state.
		gated.Group(func(r chi.Router) {
			r.Use(ri.Operator.AuthMW.RequireInternalTenant())
			r.Use(ri.Operator.AuthMW.RequirePermission("marketing.score_profile.write"))
			api := humachi.New(r, ri.APIConfig)
			handlers.RegisterScoreProfileWriteRoutes(api, m.profileHandler)
		})

		// CONFLICT RESOLVE bucket — review-queue resolution. Distinct
		// from contact.write because operators may be granted
		// queue-management authority without full record-edit access
		// (e.g. import operators who triage but don't curate contacts
		// outside of imports).
		gated.Group(func(r chi.Router) {
			r.Use(ri.Operator.AuthMW.RequireInternalTenant())
			r.Use(ri.Operator.AuthMW.RequirePermission("marketing.conflict.resolve"))
			api := humachi.New(r, ri.APIConfig)
			handlers.RegisterConflictReviewWriteRoutes(api, m.conflictReviewHandler)
		})

		// CARD TYPE WRITE bucket — Phase 4. Create/update/delete card
		// types. Distinct from contact.write because retiring or
		// reshaping a card type changes the operational catalog the
		// admin UI exposes; ops with read-only contact access can be
		// granted card-emit rights without also editing the catalog.
		gated.Group(func(r chi.Router) {
			r.Use(ri.Operator.AuthMW.RequireInternalTenant())
			r.Use(ri.Operator.AuthMW.RequirePermission("marketing.card_type.write"))
			api := humachi.New(r, ri.APIConfig)
			handlers.RegisterCardTypeWriteRoutes(api, m.cardTypeHandler)
		})

		// CARD ISSUE bucket — Phase 4. Emit a card to a person.
		gated.Group(func(r chi.Router) {
			r.Use(ri.Operator.AuthMW.RequireInternalTenant())
			r.Use(ri.Operator.AuthMW.RequirePermission("marketing.card.issue"))
			api := humachi.New(r, ri.APIConfig)
			handlers.RegisterCardIssueRoutes(api, m.cardHandler.CardHandler)
		})

		// CARD SUSPEND bucket — Phase 4. Suspend / reinstate an
		// active card. The inverse (reinstate) lives in the same
		// bucket because it is the same operator authority.
		gated.Group(func(r chi.Router) {
			r.Use(ri.Operator.AuthMW.RequireInternalTenant())
			r.Use(ri.Operator.AuthMW.RequirePermission("marketing.card.suspend"))
			api := humachi.New(r, ri.APIConfig)
			handlers.RegisterCardSuspendRoutes(api, m.cardHandler.CardHandler)
		})

		// CARD REVOKE bucket — Phase 4. Irreversible revocation.
		// Narrower than suspend because revocation has no undo: ops
		// catalogues typically constrain it to a smaller role set.
		gated.Group(func(r chi.Router) {
			r.Use(ri.Operator.AuthMW.RequireInternalTenant())
			r.Use(ri.Operator.AuthMW.RequirePermission("marketing.card.revoke"))
			api := humachi.New(r, ri.APIConfig)
			handlers.RegisterCardRevokeRoutes(api, m.cardHandler.CardHandler)
		})
	})
}
