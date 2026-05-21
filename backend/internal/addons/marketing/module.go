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
	"log/slog"

	"github.com/danielgtaylor/huma/v2/adapters/humachi"
	"github.com/go-chi/chi/v5"
	"github.com/orkestra-cc/orkestra-addon-marketing/handlers"
	"github.com/orkestra-cc/orkestra-addon-marketing/importers"
	csvimp "github.com/orkestra-cc/orkestra-addon-marketing/importers/csv"
	"github.com/orkestra-cc/orkestra-addon-marketing/models"
	"github.com/orkestra-cc/orkestra-addon-marketing/repository"
	"github.com/orkestra-cc/orkestra-addon-marketing/services"
	"github.com/orkestra-cc/orkestra-sdk/iface"
	"github.com/orkestra-cc/orkestra-sdk/module"
	"github.com/orkestra-cc/orkestra-sdk/modulegate"
)

// MarketingModule implements the Orkestra SDK Module interface for the
// marketing addon. Phase 1 wires the data layer (PR-2), CRUD handlers
// + routes (PR-3), CSV importer (PR-4), and operator nav items (PR-5).
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

	// Importer registry — exposed via NewImportService when handlers are
	// wired in Init. Phase 1 ships only the CSV adapter; future adapters
	// (excel, odoo) append to the slice without changing the wiring shape.
	importerAdapters []importers.Importer
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
		},
	}}
}

// Permissions declares the Cedar permission catalog this module
// publishes. Phase 1 ships three coarse-grained keys covering Person,
// Organization, Membership, Tag, and Custom-Field-Schema operations —
// the design document's finer-grained marketing.tag.write /
// marketing.custom_field.* permissions are intentionally folded into
// marketing.contact.* to keep the RBAC surface small until a real
// separation-of-duties requirement appears.
//
// Later phases add:
//   - marketing.import.run (PR-4, importer trigger gate)
//   - marketing.activity.read / write (Phase 2, event log)
//   - marketing.score_profile.write (Phase 2, scoring tuning)
//   - marketing.card_type.write / marketing.card.{issue,suspend,revoke}
//     (Phase 4, card lifecycle)
//   - marketing.conflict.resolve (Phase 3, review queue)
func (m *MarketingModule) Permissions() []iface.PermissionSpec {
	return []iface.PermissionSpec{
		{Key: "marketing.contact.read", Module: m.Name(), Description: "View persons, organizations, memberships, tags, custom-field schemas, and import-job audit"},
		{Key: "marketing.contact.write", Module: m.Name(), Description: "Create and update persons, organizations, memberships, tags, and custom-field schemas"},
		{Key: "marketing.contact.delete", Module: m.Name(), Description: "Hard-delete contacts (org/person cascades to memberships) and tags/schemas"},
		{Key: "marketing.import.run", Module: m.Name(), Description: "Trigger CSV/Excel/Odoo imports of contact data (separate gate from contact.write so import access can be granted independently)"},
	}
}

// Init wires the data + service + handler graph. The registry calls
// this after all dependencies have been initialized.
//
// Wiring order: repositories first (Mongo collections only), then
// services (orchestration), then handlers (HTTP/Huma adapters). The
// custom-field service is shared by both contact services because
// each calls Validate before persisting record bags.
func (m *MarketingModule) Init(deps *module.Dependencies) error {
	m.logger = deps.Logger

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

	m.importerAdapters = []importers.Importer{csvimp.New()}
	importSvc := services.NewImportService(jobRepo, orgRepo, personRepo, mshipRepo, tagRepo, m.importerAdapters...)

	m.orgHandler = handlers.NewOrganizationHandler(orgSvc)
	m.personHandler = handlers.NewPersonHandler(personSvc)
	m.membershipHandler = handlers.NewMembershipHandler(mshipSvc)
	m.tagHandler = handlers.NewTagHandler(tagSvc)
	m.customFieldHandler = handlers.NewCustomFieldSchemaHandler(cfSvc)
	m.importsHandler = handlers.NewImportsHandler(importSvc)

	m.logger.Info("Marketing module initialized")
	return nil
}

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

		// READ bucket — includes the import-job audit read surface.
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
	})
}
