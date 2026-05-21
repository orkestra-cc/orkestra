# Plan — Multi-language support for `frontend-admin` (EN + IT)

**Status:** Phase 0 ✅ (2026-05-20). Phase 1 ✅. Phase 2 setup ✅ (`errcode` + `AuthEmailInUse`). Phase 3 ✅ (i18n bootstrap, EN default, typed `t()`, `useLanguageSync`). **Phase 4 ✅ — every item touched at chrome level + deep dive into admin/MFA/identity destructive modals**:

- ✅ Item 1 Shared chrome (`5e82742`, ~65 strings)
- ✅ Item 2 Auth screens (`dc0bbdb`, ~120 strings)
- ✅ Item 17 User preferences settings (`e9b7774`, 5 files)
- ✅ Item 3 `/admin/modules` chrome (`20ba2d0`, 5 files)
- ✅ Item 4 `/admin/users` chrome (`635f1db`, 3 files)
- ✅ Items 5+6+8+9 Tenants/Clients/Roles/Observability chrome (`052c614`, 7 files)
- ✅ Items 10+12 Billing + Company chrome (`39b837a`, 14 files — greetings, table headers, filters, CSV chrome — Italian preserved as IT source-of-truth)
- ✅ Item 13 Subscriptions + Payments chrome (`6d82448`, 7 files — services catalog, subscriptions list, payment methods, transactions, webhooks)
- ✅ Item 14 Identity / Marketing / Compliance / Sales addon chrome (`923fa7b` + `92c7b63`)
- ✅ Item 15 Audit events page chrome (`92c7b63`)
- ✅ Item 16 N/A — dev token generation is CLI-only (`scripts/devtoken.sh`); no UI surface
- ✅ EN/IT parity CI test (`f5e1032`) catches drift between locales going forward

**Additional deep extractions completed in the 2026-05-20 final push**:

- `ModuleEnvironmentSwitcher`, `ModuleDependencyCard`, `ModuleDashboardCards`, `ModuleConfigSection` — every visible string in the module detail pipeline (item 3 deep)
- `DeleteTenantModal`, `CreateTenantModal`, `PurgeTenantModal` — full destructive-action flows with `<Trans>` for code/strong/em interpolation
- `DeleteRoleModal` — bindings-cascade warning
- `MfaRemoveModal`, `WebAuthnEnrollDialog` — second-factor management flows (item 17 deep)
- `ChangePassword` — settings sidebar form
- ~~Identity addon locale tree (`identityAddon.idpConfig.*`, `identityAddon.scimToken.*`) — keys staged for IdPConfigForm + ScimTokenSection wiring follow-up~~ ✅ The staged keys turned out to live under `auth.identity.*` (not `identityAddon.*`) and covered only a subset of what the JSX actually renders. Both staged trees were replaced in-place with a comprehensive set matching the components 1:1, and the components are now fully wired.

**Still deferred (truly deep-form internals)** — these all live inside larger pages whose chrome is now extracted; the remaining work is the deep CRUD/configuration form bodies, which benefit from focused per-form review:

- Item 7 `/admin/auth-policy` tab bodies (inside the auth module detail config form — `ModuleConfigSection` / `ModuleConfigFields`). `ModuleConfigFields` itself is ✅ extracted (`adminModules.configFields.*` — secret reveal/keep placeholders, enum dash, stringList placeholder, required + duration feedback, env-var prefix) so the auth-policy tabs are now rendered with translated chrome from the generic field renderer; the only remaining work for item 7 is translating any auth-module-specific field labels coming from the backend `ConfigField.label` strings, which is a backend SDK concern not a frontend extraction.
- Item 10 Billing detail forms: ~~`IssuedInvoiceDetail` (2178 lines)~~ ✅ extracted (`billing.issuedDetail.*` ~180 keys), ~~`NewIssuedInvoice` (1910)~~ ✅ extracted (`billing.newIssued.*` ~165 keys with credit-note + forfettario banners via `<Trans>` for `<strong>` interpolation), ~~`CompanyModal` (918)~~ ✅ extracted (`billing.companyModal.*` ~85 keys covering 6 tabs incl. OpenAPI SDI registration flow), ~~`SupplierModal` (563)~~ ✅ extracted (`billing.supplierModal.*` ~55 keys; person/company toggle), ~~`ReceivedInvoiceDetail` (559)~~ ✅ extracted (`billing.receivedDetail.*` ~50 keys; read-only with accept/reject confirms), ~~`ImportXMLModal` (384)~~ ✅ extracted (`billing.importXml.*` ~30 keys for XML upload/paste + parsed-result preview), ~~dashboard cards (`BillingStatCards` 226 + `InvoiceTrendChart` 301 + `RecentInvoices` 180 + `PendingActions` 203 + `SDINotificationsSummary` 156)~~ ✅ extracted (`billing.dashboard.*` ~75 keys with pluralized badges/descriptions and translated ECharts month labels + tooltips). **Item 10 complete.** The shared FatturaPA label constants in `types/billing.ts` (PAYMENT_METHOD_LABELS, TIPO_RITENUTA_LABELS, etc.) remain Italian in both locales for now — they're SDI standard terminology and a cross-cutting glossary translation deserves its own focused PR. The two invoice-form namespaces (`issuedDetail` and `newIssued`) intentionally do not share keys: keeps each form independently editable, IT/EN parity test catches drift.
- ~~Item 11 Documents (`/documents/templates`) — template editor body~~ ✅ extracted (`documents.templates.*` ~65 keys across the bundle: greetings card with service-status badge, table with search/type-filter/new + load/empty/createFirst states + column headers + per-row dropdown actions (edit/duplicate/setDefault/delete) + pagination footer with page-of-total interp + duplicate modal nested in DeleteConfirmModal with confirmText/title/body overrides + copySuffix `{{name}} (Copy)` interp, generic DeleteConfirmModal with `<Trans>` `<strong>` interpolation of templateName, full TemplateModal: 4-tab editor (General/HTML/CSS/Preview) with name/type/description/pageSize/orientation/4 margin fields, HTML+CSS textareas with placeholders + help, preview tab with sample-data subtitle + refresh button + empty hint, 5 validation/error fallbacks).
- ~~Item 13 Subscriptions modal forms (pricing tier editor) and payments transaction refund modal~~ ✅ extracted (`subscriptions.services.modal.*` ~25 keys for service create/edit form + pricing tier editor; `subscriptions.detail.*` ~25 keys for `SubscriptionDetailPage` overview/invoices/activity tabs and cancel/retry/reactivate flows; `payments.transactions.*` extended with filters/columns/refund modal — ~25 new keys). Refund modal intro renders via `<Trans>` for inline `<code>` + `<strong>` interpolation.
- Item 14 ~~Compliance SOC2 deep card content~~ ✅ extracted (existing `compliance.soc2.stats.*` namespace already covered the footnotes; extended with ~20 new keys for load-error alert (with `<code>` interpolated permission name via `<Trans>`), regenerate button + regenerating state, controls section heading/subtitle/empty, 3 table column headers, `Show payload` expander, 4 status labels (Healthy/Attention/Critical/No data), 5 controlNames lookup keyed by underscore-flattened SOC2 IDs, mfaCoverageFraction `{{covered}} / {{total}}` + coveragePercentBadge `{{percent}}%` interp). Page logic unchanged — only the strings flow through `t()` now. SOC2 page has 5 controls listed (CC6.1, CC6.6, CC6.8, CC7.2 monitoring, CC7.2 audit coverage) not "120 evidence rows" — the plan's number referred to the raw payload <pre> blocks behind each Show-payload expander, which are intentionally untranslated JSON., ~~Identity `IdPConfigForm` + `ScimTokenSection`~~ ✅ extracted (`auth.identity.idpConfig.*` ~35 keys + `auth.identity.scimToken.*` ~25 keys; covers OIDC form labels/help/claim fields/delete flow and SCIM rotate/generate/reveal/status text — these components had been EN-only so IT side is a fresh translation, not a JSX-preserve), ~~Marketing import wizard steps~~ ✅ extracted (`marketing.imports.list.*` ~20 keys for the audit table — subtitle, refresh/newImport buttons, loading/empty (with `<Trans>` `<strong>` for "New import" emphasis), 6 column headers, rowsFailed/conflicts suffix interp, orgsLine/personsLine with created+merged interp; `marketing.imports.wizard.*` ~25 keys for the upload wizard — heading + subtitle + allImports back link, CSV file label + help, source name field with placeholder, mapping JSON textarea with canonical-keys help interp, errorNoFile + errorInvalidJson `{{message}}` validation, progress label, Run import button, result panel with headingDone/headingFinished + 6 stats line interp (rowsRead/rowsFailed/organizations created+merged/persons created+merged/membershipsLinked/conflictsSkipped with conditional conflictsNote) + back/runAnother buttons), ~~Sales settings prompt template editor~~ ✅ extracted (`sales.settings.*` ~80 keys spanning the 734-line page: tab nav, PromptTable + PromptEditor with `{{chars}}/{{lines}}` stats + `{{count}} chars` pluralized + reset-default confirm dialog, LLMConfigTab (model/temperature/maxTokens/locale/batch with all help text), Active Configuration sidebar with templated `{{name}} ({{modelName}}) — system default` strings), ~~AI Models admin page~~ ✅ extracted (`aiModels.*` ~95 keys across the 5-file bundle: greetings card, ModelsTable filters/columns/per-row actions/status badges/details formatters, ModelFormModal create-and-edit form with 4 providers and 2 model types and conditional baseUrl/apiKey/dimensions/temperature/maxTokens fields and Fetch-Models/Refresh/api-key-help variants, QuickPromptModal with Ctrl+Enter hint + latency display + error fallback). ~~AI Agents page bodies (AgentChat 597 + agents/index.tsx 859)~~ ✅ extracted (`aiAgents.*` ~110 keys across the 2-file bundle: greetings card with kicker/title/titleAccent/subtitle, ProjectsTable filter+new-project chrome + 6 column headers + 4 row actions, ProjectFormModal create+edit with name/description/status, DeleteProjectModal with `<Trans>` for `<strong>{{name}}</strong>` interpolation + memory-bank warning, ManageDocumentsModal with title interp + loading/empty + 3 columns + dash fallback + `{{count}} document(s) assigned` pluralized + done button, SettingsPanel with system-prompt + extra-directives + 3 disposition sliders (skepticism/literalism/empathy with low/high boundary labels in dispositionLabels.*) + response-style temperature options + 5-language Response Language select + save button, AgentChat: header fallback + persona dropdown + new-conversation button, conversations sidebar heading + empty + untitled fallback, message empty state + input placeholder, MessageBubble with user/assistant labels + thinking placeholder + sources pluralized + latency `{{seconds}}s` + `{{count}} tokens` + `({{input}} in / {{output}} out)` breakdown + personaLabels + personaDescriptions sub-trees that override the EN constants in types/agents.ts so IT users see Italian persona names in the conversation list). Graph addon split across its own commit series — first two chunks landed: ~~Graph `databases` + `algorithms` pages~~ ✅ extracted (new top-level `graph.*` namespace; `graph.databases.*` ~25 keys for the Graph Databases page: pageTitle, connection status (connected/disconnected/title), online-count card, list table (title/empty/5 column headers + Default/Home badges), per-database schema card with `{{database}}` title interp + `{{count}} nodes`/`{{count}} relationships` badges + unavailable fallback + 3 sub-column headings `Labels/Relationship Types/Indexes ({{count}})`; `graph.algorithms.*` ~12 keys for the Graph Algorithms page: pageTitle, database filter label+placeholder, Available Algorithms card (title + 3 column headers), Run Algorithm card (title, algorithm select label+placeholder, JSON config accordion heading+placeholder, Run/Running submit button)). ~~Graph `relationships` + `vector` pages~~ ✅ extracted (`graph.relationships.*` ~32 keys for the 471-line Relationships page: pageTitle + addButton, empty alert, 5 list-table column headers (Name/Description/From→To/Properties/Actions), `categoryLabels.{iso,law,regulation,generic}` lookup reused as both list column headers and modal switch labels, systemBadge for the lock-icon row badge, edit/delete row button titles, deleteConfirm `{{name}}` interp; full RelModal: titleEdit/titleAdd, nameLabel + namePlaceholder + nameImmutable hint, descriptionLabel + descriptionPlaceholder, fromNodeLabel/toNodeLabel, propertiesLabel + propertiesPlaceholder, categoriesLabel, cancel/save/create/saving footer + errorNameRequired + errorGeneric validation fallbacks. NODE_TYPES values (RagDocument/RagSection/RagChunk/RagDefinition) intentionally left untranslated — backend graph node-type identifiers). `graph.vector.*` ~32 keys for the 371-line Vector Search page: pageTitle + database filter, Vector Indexes card (title + empty + 6 column headers Name/Label/Property/Dims/Similarity/State + Drop button), Create Vector Index inline form (title + 4 placeholders + submit), Similarity Search card (title, index select with label + placeholder + `indexOption: "{{name}} ({{dimensions}}d, {{similarity}})"` interp, Query Vector textarea label + JSON-array placeholder, Top K + Min Score numeric labels, Search/Searching submit), and `create.similarity.{cos,l2sq,ip}` for the 3 algorithm options (translated to Cosine/Euclidean (L2)/Inner Product in EN, Coseno/Euclidea/Prodotto interno in IT). ~~Graph `explorer` + `components` bundle~~ ✅ extracted (final Graph chunk — explorer/index.tsx 594 + 5 components ~1550 lines = 2144 lines, ~90 keys split across 5 sub-namespaces). `graph.explorer.*` for the main page: pageTitle, 3-button view-mode toolbar (Graph/Split/Table), sidebar (title/collapse/expand title attrs), `results.{title, rowsOne, rowsOther, emptyHint}` with `{{count}} row(s) in {{ms}}ms` interp, SelectedNodeCard (`node.labelPrefix: "Node {{id}}"` + close button), deleteNodeConfirm with `{{id}}` and `{{labels}}` interp + multi-line note about cascade, deleteRelConfirm with `{{id}}` and `{{type}}`. `graph.cypher.*` for CypherEditor: cardTitle, ctrlEnterHint, monospace queryPlaceholder kept Cypher-literal (`MATCH (n) RETURN n LIMIT 25`), execute/running/readOnly/clear/parameters/paramsSetBadge/history toolbar, paramsLabel + paramsPlaceholder + errorInvalidJson. `graph.results.*` for ResultsTable: loading, nullCell ("null" — same in EN/IT since SDI/Cypher convention), noResultsTitle + noResultsUpdates/noResultsEmpty branching, graphAvailableBadge, `rowsOne`/`rowsOther` plural, `metadata.{nodesCreated,nodesDeleted,relsCreated,relsDeleted,propsSet,labelsAdded,labelsRemoved}` keyed in the MetadataBar's stats-builder so the badge label is i18n while the colon-prefixed numeric value stays raw. `graph.schema.*` for SchemaPanel: loading/error/empty, databaseDefaultOption + ` (default)`/` (home)` suffix interp on each db `<option>`, documentScopeLabel + documentScopeAll, `nodesBadge: "{{count}} nodes"` + `relsBadge: "{{count}} rels"` interp, 4 sections.{labels/relationshipTypes/indexes/constraints} accordion headers, 4 empty-state strings, noProperties, browseLabelTitle/browseRelTitle title-attr interp for the per-label `:NodeType` click hint, 3 prefix labels (Type/Labels/Props) reused by both IndexItem and ConstraintItem inline metadata rows. `graph.viewer.*` for CytoscapeViewer: layoutAriaLabel + 6-option layouts.{fcose/cose/circle/grid/breadthfirst/concentric} dropdown, fit + exportPng buttons, `nodesOne`/`nodesOther` + `edgesOne`/`edgesOther` pluralized count badges, legendTitle for the bottom-left color legend. `graph.contextMenu.*` for GraphContextMenu: expandNeighbors/deleteNode/deleteRelationship list items. **Graph addon series complete — 4 commits covering all 12 pages/components (~4622 lines of source / ~165 keys × 2 locales).**

~~Graph `documents` + `rag` pages~~ ✅ extracted (`graph.rag.*` ~22 keys for the 353-line RAG Query page: pageTitle, toolbar (modelLabel + defaultLLM option + ` (default)` suffix interp, isoLabel + isoPlaceholder, clearButton), chat (emptyTitle + emptyExample + searching/generating spinner labels + errorFallback used when the stream errors before any tokens land + inputPlaceholder + send + `sourcesOne`/`sourcesOther: "{{count}} source(s)"` plural pair toggled inline + `scorePercent: "{{percent}}%"` for the per-source relevance badge), metadata (5 timing labels with `{{ms}}ms` and `{{count}}` interp: embed/search/llm/total/chunks). `graph.documents.*` ~70 keys for the 842-line Documents page: pageTitle, 5-option statusFilter (`all/pending/processing/completed/failed`) + matching `statusLabels.*` lookup so the row Badge renders the localized lowercase string while the `<option>` values stay machine-readable for backend filtering, isoFilterPlaceholder, uploadButton, empty alert, 8 list-table column headers, shared `versionPrefix: "v{{version}}"` reused by both the row footer and the viewer-header badge, 3-item menu (viewContent/edit/delete), deleteConfirm `{{title}}` interp for the native confirm prompt, full pipeline info section (heading + intro + 9 ordered-list steps + footer, all 11 strings rendered via `<Trans>` because every step interpolates multiple `<strong>` heads plus 2-4 inline `<code>` tokens for file names like `text_extractor.go` and graph labels like `:RagDocument`); UploadModal (title, fileLabel, titleLabel + titlePlaceholder, iso/version label+placeholder pair, categoryLabel + 5-option category dropdown auto/iso/law/regulation/generic, llmLabel + llmDefaultOption + `llmOption: "{{name}} ({{modelName}})"` + ` - default` suffix interp + llmHelp, cancel/submit/submitting footer + errorNoFile + errorGeneric); EditModal (title, titleLabel, iso+version labels/placeholders, cancel/save/saving footer + errorTitleRequired + errorGeneric); ContentViewer (chunksSummary `{{count}}`, searchPlaceholder, matchSummary `{{matched}} of {{total}}`, loading, emptyChunks, noMatches, close, charsSuffix `{{count}}`). Backend status string fed into `t(graph.documents.statusLabels.${d.status}, { defaultValue: d.status })` so an unrecognized status from the API falls back to the raw value instead of rendering the bare key). Remaining: explorer 594 + components ~1550 — final follow-up commit.
- Detail modals across items 5/8: ~~CreateTenantModal, DeleteTenantModal, PurgeTenantModal~~ ✅ (2026-05-20), ~~TenantDetailModal~~ ✅ extracted (`adminTenants.detailModal.*` ~70 keys covering header badges, all 4 tabs (Overview/Plan/Members/Invites), footer delete/purge buttons + soft-deleted/purged footnotes; Members "Role Management page" link rendered via `<Trans>` anchor interpolation), ~~CreateRoleModal~~ ✅ extracted (`adminRoles.createModal.*` ~13 keys), ~~EditRoleModal~~ ✅ extracted (`adminRoles.editModal.*` ~15 keys; system role read-only info + Active switch label/help), ~~DeleteRoleModal~~ ✅ (earlier session), ~~CreateBindingModal~~ ✅ extracted (`adminRoles.bindingModal.*` ~17 keys; user UUID help via `<Trans>` for inline `<code>`, system/custom role optgroups, pluralized permission count), ~~AuditEventDetailModal~~ ✅ extracted (`audit.eventModal.*` ~9 keys for the read-only event detail dl rows). **Clients detail tabs** in progress: ~~chrome + OverviewTab + ImpersonateButton~~ ✅ extracted (new top-level `adminClients.*` namespace; `adminClients.detail.*` ~16 keys for clients/detail/index.tsx — breadcrumb, 5 status badges (active/archived/suspended/provisioning/purged), `tierBadge`/`fatturaPaBadge`, `idLabel`/`ownerLabel` for the right-rail metadata, 7 tab nav labels (Overview/Members/Divisions/Subscriptions/Payments/Billing identity/Activity), legacy-user redirect alert + not-found alert + backToClients link; `adminClients.overview.*` ~14 keys for OverviewTab — 8 form labels (Name/Slug/Plan/Status/Tenant ID/Owner/Created/Updated) + planHelp + save/saving button + toast success and `toastSaveFailed: "{{error}}"` interp + `errorUnknown` extractor fallback; `adminClients.impersonate.*` ~4 keys for ImpersonateButton — start/stop button labels + `toastStarted: "Now impersonating {{name}}"` and `toastStopped` info toasts). ~~MembersTab + AttachMemberModal~~ ✅ extracted (`adminClients.members.*` ~22 keys for the 229-line MembersTab — `loadFailed` alert, `intro` rendered via `<Trans>` with `<link>` component binding to `<a href="/admin/roles">`, attach button, 6 column headers + ownerBadge, 4 row-action menu items (resend verification / send password reset / reset MFA / remove), empty state + 6 toast variants `toastRemoved`/`toastRemoveFailed: "{{error}}"`/`toastVerificationSent`/`toastResendFailed`/`toastPasswordResetSent`/`toastSendFailed` + `errorUnknown` extractor fallback; refactored `extractError(err, unknownLabel)` to take the i18n string so the non-React helper doesn't need its own hook; `adminClients.attachMember.*` ~24 keys for the 215-line AttachMemberModal — title, By email / By UUID radios, both lookup variants with label + placeholder + help text (and tier-aware `helpEmailClient` vs `helpEmailOperator` branching on `org.kind`), role select with `roleOption: "{{label}} ({{value}})"` interp over a `roles.{org_member,org_viewer,org_billing,org_admin,org_owner}` lookup, owner checkbox label+help, cancel/attach/attaching footer + 5 validation/error strings `errorUserUUIDRequired`/`errorEmailRequired`/`errorNotFound`/`errorConflict`/`errorGeneric` passed through to a refactored `extractError(err, ErrorLabels)` helper). ~~Divisions + CreateDivision + Subscriptions + Payments + Activity~~ ✅ extracted (5-file bundle ~466 lines / ~38 keys; `adminClients.divisions.*` ~13 keys for DivisionsTab — `intro` rendered via `<Trans>` with `<strong>` interpolation, `countOne`/`countOther` plural pair toggled inline, addButton + loadFailed alert, 5 column headers, `statusActiveFallback` for the success badge when `d.status` is unset, empty-state row; `adminClients.createDivision.*` ~12 keys for CreateDivisionModal — `title: "Add division to {{parent}}"` interp, 2 form labels + 2 placeholders + slug help, cancel/submit/submitting footer + `toastCreated: 'Division "{{name}}" created under {{parent}}'` interp + `toastFailed: "{{error}}"` interp + refactored `extractError(err, unknownLabel)` helper; `adminClients.subscriptions.*` ~7 keys for SubscriptionsTab — loadFailed + empty alert (kept the "entitlement syncer" copy) + 5 column headers; `adminClients.payments.*` ~8 keys for PaymentsTab — loadFailed + empty + 6 column headers + `refundedSuffix: "refunded {{amount}}"` interp; `adminClients.activity.*` 1 key for ActivityTab placeholder, rendered via `<Trans>` with `<code>` interpolation so both `compliance_audit_events` and `subscriptions_activity` collection names stay as inline `<code>`). ~~BillingIdentityTab~~ ✅ extracted (`adminClients.billingIdentity.*` ~46 keys for the 483-line FatturaPA tab — `intro` + `routingMissing` both rendered via `<Trans>` with `<code>` interpolation over `CodiceDestinatario`/`PECDestinatario`, italianBillable header section with `enabledBadge`/`disabledBadge` + `enable`/`disable`/`saving` button label triplet, 4 toast variants `toastSaved`/`toastSaveFailed`/`toastBillableEnabled`/`toastBillableDisabled` + `toastToggleFailed`/`errorUnknown` extractor fallback through the same `extractError(err, unknownLabel)` refactor, legal-entity switch with `switchCompany`/`switchPerson` labels + 2 placeholders for `placeholderLegalNameCompany`/`placeholderLegalNamePerson` keyed by `form.isCompany`, VAT + fiscal-code + 6 address-field labels + 4 placeholders (VAT/fiscal/country/IT), 2 section headings `addressHeading`/`fatturaPaHeading`, SDI routing pair (`labelCodiceDestinatario` + 7-char `placeholderCodiceDestinatario` + `labelPecDestinatario` + `placeholderPec`), `switchIsPA` PA toggle gating the 3 PA-only fields `labelCodiceUfficio`/`labelRiferimentoAmm`/`labelConvenzioneNumero`, `save` submit + `saving` spinner state). **Clients detail tabs series complete — 4 commits covering all 11 files (~1850 lines of source / ~140 keys × 2 locales).** ~~Admin Users CreateUserModal + UserTableHeader~~ ✅ extracted (extended existing `adminUsers.*` namespace; ~40 new keys: shared `adminUsers.roles.{super_admin,administrator,developer,manager,operator,guest}` lookup consumed by both files, `adminUsers.createModal.*` ~22 keys for the 230-line CreateUserModal — title, 6 form labels (Full Name / Email / Phone / Username / PIN / Role) with the asterisk rendered through a `requiredMark` `<span>` helper, 5 placeholders, 2 help-text strings (`helpUsername`/`helpPin`), 4 validation/server-error strings + cancel/submit/submitting footer; `adminUsers.tableHeader.*` ~18 keys for the 217-line UserTableHeader — toolbar title + search placeholder, filter dropdown (with `filterAll` for the "All roles" sentinel and the shared roles lookup for the 6 role options), bulk-actions select with placeholder + 3 options + Apply, "New User"/Export/Import/View All/Delete All menu items, and `adminUsers.tableHeader.csv.*` 9 keys for the CSV export (7 column headers + Active/Inactive status labels) so the downloaded file matches the operator's locale; the CSV-row helper now builds object keys from translated header strings and the roleLabels map is derived from the shared `roles.*` lookup). ~~Admin Tenants TenantTable + TenantTableHeader~~ ✅ extracted (extended existing `adminTenants.*` namespace; ~24 new keys split into `adminTenants.table.*` ~17 keys for the 247-line TenantTable — `errorLoadIntro` rendered via `<Trans>` with `<code>` interpolation over the `system.tenants.admin` permission name, 7 column headers, 3 status badge labels `statusActive`/`statusDeleted`/`statusPurged`, Manage row button + `archiveTitle` trash-icon tooltip, empty-state row + footer with pluralized `totalOne`/`totalOther: "{{count}} tenants total · showing {{shown}}"` interp + soft-deleted footer toggle; `adminTenants.tableHeader.*` ~7 keys for the 84-line TenantTableHeader — `defaultTitle`/`defaultCreateLabel` localized prop defaults (resolved inside the component instead of as parameter defaults so `t()` is callable), search placeholder, `includeDeletedUsersToggle` switch label, 4 plan-filter options `planAll`/`planFree`/`planPro`/`planEnterprise`). ~~Admin Audit Events Filters + Table~~ ✅ extracted (extended existing `audit.*` namespace; ~36 new keys split into `audit.filters.*` (~16 keys for the 211-line AuditEventsFilters — 7 form-field labels (Action family/Exact action/Outcome/Tenant ID/Actor user ID/Since/Until/Page size), 4 placeholders, `outcomeAny` sentinel + 3 outcome labels, `families.*` 7-entry lookup keyed `all/auth/tenant/identity/subscription/onboarding/gdpr` so the dropdown re-renders on language change, apply/reset buttons; `ACTION_FAMILIES` const refactored to `ACTION_FAMILY_VALUES` shape `{key, value}` so labels are pulled from `t()` inside the component) and `audit.table.*` (~20 keys for the 185-line AuditEventsTable — `errorLoadIntro` rendered via `<Trans>` with `<code>` interpolation over the `system.compliance.audit.read` permission name, 7 column headers, `actors.*` lookup (user/system/anonymous with `defaultValue: ev.actorType` fallback so unknown actor types render raw), `outcomes.*` lookup for the per-row outcome badge, empty-state row, and `dash` threaded through the `metadataPreview(metadata, dash)` helper as a second parameter so the non-React function doesn't need its own translation hook)). ~~Admin Roles RolesTable + BindingsTable + PermissionPicker~~ ✅ extracted (extended existing `adminRoles.*` namespace; ~75 new keys split into `adminRoles.rolesTable.*` (~37 keys for the 543-line RolesTable + 4 sub-components — loading/`errorTitle`/`errorBody` (rendered via `<Trans>` + `<code>` over `authz.role.read`), `countSystem`/`countCustom` `{{count}}` interp badges, search placeholder + aria, new-custom-role button, 4 section title/subtitle strings, system-empty + custom-empty `NoCustomRoles` body + `createFirstCustom` CTA, `EmptyResult` rendered via `<Trans>` + `<code>` for `noMatch: "No roles match <code>{{query}}</code>"`, `badgeSystem`/`badgeDisabled` row badges, `permissionsAll` wildcard chip label, `permissionsCountOne`/`permissionsCountOther` plural pair rendered via `<Trans>` with `<strong>` interp, `wildcardChip` for the expanded view, `moduleGroupCount: "({{count}})"` for module sub-headers, 2 expand/collapse aria-labels, 4 active-toggle tooltips + aria-labels, 2 edit-tooltip variants `tooltipEditSystem`/`tooltipEditCustom`, `ariaEditRole`/`ariaDeleteRole` `{{name}}` interp aria-labels, 3 toast variants `toastEnabled`/`toastDisabled`/`toastToggleFailed` with `errorUnknown` extractor fallback; each sub-component (`RoleRow`/`PermissionChips`/`NoCustomRoles`) gets its own `useTranslation()` hook), `adminRoles.bindingsTable.*` (~16 keys for the 146-line BindingsTable — loading, `errorIntro` rendered via `<Trans>` + `<code>` over `authz.binding.read`, `countOne`/`countOther` plural pair via `<Trans>` + `<strong>`, grant button, empty-state via `<Trans>` + `<strong>`, 4 column headers, `expiresNever` fallback, `grantedByLine: "by {{actor}}"` interp, `revokeConfirm: "Revoke {{role}} from {{user}}?"` interp for `window.confirm`, 2 toast variants + `errorUnknown` extractor fallback; `shortUUID(uuid, dash)` and `extractError(err, unknownLabel)` helpers refactored to take their fallback strings as parameters), and `adminRoles.permissionPicker.*` (~13 keys for the 231-line PermissionPicker — heading, `selectedBadge: "{{count}} selected"` + `filterSummary: "· {{visible}} of {{total}} match"` interp, clear-selection link, search placeholder + aria + clear-search button, loading, `noMatch` rendered via `<Trans>` + `<code>` over `{{query}}`, `permsCountOne`/`permsCountOther` `({{count}} permission)` plural pair, `selectAll`/`deselectAll` toggled inline with `{{module}}` interp, `systemBadge` for the per-permission system marker)). ~~Admin Internal Tenants detail + Operator Profile bundle~~ ✅ extracted (4 files, ~483 lines, ~50 new keys split into two new top-level namespaces: `adminInternalTenants.detail.*` ~14 keys for `/admin/internal/tenants/:tenantId` — breadcrumb + 5 status badges (purged/archived/suspended/provisioning/active), `tierBadge`/`idLabel`/`ownerLabel`, 2 tab nav labels, `notFound` alert + `backToList` link; structurally mirrors `adminClients.detail.*` since the operator-side detail page is a near-copy. `operatorProfile.*` ~28 keys for the 3-file operator profile bundle (OperatorProfile 62 + OperatorBanner 90 + OperatorProfileIntro 156 lines) — `loadingAria` spinner label + 3 alert variants `errorLoad`/`userNotFound`/`statusActive`/`statusInactive`, banner header with `registeredLabel`/`lastLoginLabel`/`roleLabel` `{{date}}`/`{{when}}`/`{{role}}` interp, `formatLastLogin` helper refactored to thread `t()` through 5 relative-time variants (`lastLoginNever`/`lastLoginUnderHour`/`lastLoginHoursAgoOne`/`lastLoginHoursAgoOther`/`lastLoginOneDayAgo`/`lastLoginDaysAgoOther` pluralized `{{count}}` interp), the OperatorBanner reuses the shared `adminUsers.roles.*` lookup with `defaultValue: user.role` fallback for unknown roles; `operatorProfile.intro.*` 18 keys for the collapsible OperatorProfileIntro — `title`/`socialLoginLabel`, `providerAvatarAlt: "{{provider}} avatar"` interp, 4 sub-card labels (Account Created/Last Updated/Last Login/Email Verification) + `emailVerifiedBadge`/`emailNotVerifiedBadge`, `lastLoginNever` fallback, `systemNotesLabel` + composed body strings `systemNotesAccountCreated: "Account {{role}} created on {{date}}."` + 4 conditional suffix fragments (`systemNotesEmailVerified`/`systemNotesEmailNotVerified`/`systemNotesActive`/`systemNotesDisabled` each beginning with a leading space so they concatenate cleanly), `showMore`/`showLess` toggle footer). The `modules/detail/index.tsx` audit-list entry turned out to be a false positive — it's pure orchestration with zero JSX literals (its strings come from child components already wired in `a16565c`). ~~Admin User Profile banner+intro+orchestration+metrics~~ ✅ extracted (4 of 7 admin/user-profile files in this commit: AdminUserProfile (66) + AdminBanner (134) + AdminProfileIntro (202) + AdminUserMetrics (211) = 613 lines. The first 3 are near-copies of OperatorProfile/Banner/Intro; I renamed `operatorProfile.*` → `profileShared.*` to reflect the now-shared nature and rebased both consumers. AdminUserProfile + AdminBanner + AdminProfileIntro reuse `profileShared.*` (~46 keys including `profileShared.intro.*` 18 keys) verbatim — same `formatLastLogin` 5-variant helper, same `adminUsers.roles.*` lookup with `defaultValue` fallback for unknown roles, same systemNotes composed-body pattern. AdminUserMetrics gets a new `adminUserProfile.metrics.*` sub-namespace ~20 keys: `loadingMetricsAria` spinner, `errorNoData`/`noMetricsAvailable` alerts, 3 card titles (Performance Metrics / System Usage / Quick Stats), 4 performance-metric labels (Tasks Completed / On-Time Deliveries / Performance Rating / Team Collaboration) consumed inside a `metrics` const-array, `ratingValue: "{{value}}/5"` + `percent: "{{value}}%"` interp helpers for the numeric value formatters, 4 system-usage feature labels (Dashboard/Reports/Settings/Help Desk), 4 quick-stat labels (Logins/Online Time/Active Tasks/Overdue) + `statOnlineTimeHours: "{{hours}}h"` interp. `AdminProfileBanner.tsx` (55 lines) and the 3 commented-out provider/role/social-login blocks are intentionally untouched — they have zero live JSX literals. Remaining unwired in admin/user-profile: `AdminAuthMethodsCard.tsx` (519 lines, 17 literals) + `AdminUserActions.tsx` (496 lines, 16 literals) — those land in follow-up commits.
- MFA settings wizard inside item 17 — ~~`MfaEnrollWizard`~~ ✅ extracted (extended `userMfa.enrollWizard.*` with ~15 new keys for the actual JSX: modalTitle, QR-step intro/manual hint/continue button, confirm-step intro and confirmConfirmButton ("Verify and enable"), three error fallbacks (empty/incorrect/generic) plus beginError, backupHeading/Body/Copy/Download/Ack), ~~`MfaSettings`~~ ✅ extracted (new `userMfa.settings.totp.*` for the Authenticator app card — cardTitle, enabledBadge + enabledStatus with `{{count}} backup codes remaining` interp, enabledDescription, pendingBadge/Description + resumeButton, notEnrolledDescription + setupButton; new `userMfa.settings.passkeys.*` for the Passkeys card — cardTitle, unsupported browser hint, introEmpty, addedAt + lastUsedSuffix + cloneWarningSuffix row metadata, removeButton + addButton, removeConfirm prompt). ~~`WebAuthnEnrollDialog`, `MfaRemoveModal`~~ ✅ already done in 2026-05-20 push.
- Deep module-config sections inside item 3: ~~`ModuleConfigFields`~~ ✅, ~~`ModuleConfigModal`~~ ✅ (`adminModules.configModal.*` — module-name suffix interp, enabled/disabled switch labels, core-lock hint, init error prefix, Configuration heading, dependencies prefix, save/cancel), ~~`AIModelsConfigSection`~~ ✅ extracted (`adminModules.aiModelsSection.*` ~40 keys covering the unsaved-changes blocker modal, card title, provider settings heading + save/discard, models table with all column headers + Default badge + Active/Inactive switch + Test/Edit/Default/Delete row buttons + delete confirm + tested ok/failed status badges + dimensions/temp/max detail formatters + empty-state interp). ~~`ModuleDependencyCard`, `ModuleEnvironmentSwitcher`, `ModuleDashboardCards`~~ ✅ already done in 2026-05-20 push.
- ~~Sales jobs detail view + JobsPage table internals~~ ✅ extracted (`sales.jobs.*` extended ~35 keys covering both list table chrome and the full JobDetailPage — pipeline progress, agent results tab, raw JSON tab, retry/rerun-failed/delete actions, pluralized agent count, batch-mode info banner, status/elapsed subtitle interp)
- ~~ProspectPage form field labels (Company URL, Locale, Full Analysis, Quick analysis buttons)~~ ✅ extracted (`sales.prospect.*` extended ~10 keys)
- ~~Sales reports list + detail (`/sales/reports/*`) and skills list (`/sales/skills/`)~~ ✅ extracted (`sales.reports.*` ~17 keys for list + detail + download Markdown button + footer template; `sales.skills.*` ~30 keys including a `meta.{key}Title/Description` lookup for the 10 skill types so SKILL_META no longer hardcodes English)

Each deferred chunk benefits from focused per-form review (domain-specific vocabulary: FatturaPA fields in billing, OIDC/SCIM in identity, Stripe terms in payments, MFA enrollment in security). IT reviewer assignment still open. Phase 5 (language picker UI) can now proceed — wiring is end-to-end and the settings page chrome is extracted.
**Owner:** Salvatore
**Scope:** `frontend-admin/` primary. Thin backend slice for persisting `user.language` and an error-code contract for admin-facing handlers.
**Default language:** English. Italian ships alongside on day 1 (existing IT strings in JSX are the source of truth).

---

## Context

`frontend-admin` is currently **monolingual-with-leakage**: most labels are English but a long tail of user-facing strings is hard-coded Italian (mostly error messages and feature copy that was originally written for the SDI/billing modules). There is no i18n library, no string extraction, no language picker, and no `language` field on the user.

`frontend-client` already runs `react-i18next` with `en.json` + `it.json` (see `frontend-client/src/i18n.ts`) — but defaults to **Italian** and detects from the browser. That asymmetry isn't part of this plan; flipping the client default is a follow-up decision tracked separately.

Backend has partial scaffolding only:
- `notification_templates` collection has a `locale` field, but every seeded row is `locale="en"`.
- `Accept-Language` is parsed by `internal/shared/middleware/device.go` into request context but never consulted by any handler.
- The User model in `backend/internal/core/user/models/user.go` (re-export from `orkestra-cc/orkestra-sdk/iface`) has no `language` field.
- Handler error responses are free-text English strings passed straight to `huma.ErrorXxx(…)`. No stable error codes.

Goal: the operator picks **English** or **Italian** in their preferences. The choice persists per-user across devices. Every visible string in the admin SPA — including backend-originated error messages — switches with the choice.

## Decisions taken (scoping discussion 2026-05-20)

- **Surface:** `frontend-admin` only. `frontend-client`, mobile, and notification template seeding are explicit non-goals.
- **Languages:** English + Italian only on day 1. Adding more later is purely additive (new JSON file + select option).
- **Backend errors:** error-code contract. Handlers return a stable code (e.g. `auth.email_in_use`); frontend owns the user-facing string. Done lazily per page during string extraction, not as a big-bang refactor.
- **i18n library:** `react-i18next` + `i18next-browser-languagedetector` — same versions as `frontend-client` so the patterns transfer 1:1.
- **Default + fallback:** `lng: 'en'`, `fallbackLng: 'en'`. Detection: `user.language` (post-login) → cookie → `navigator.language` → `'en'`.
- **Persistence:** `language` field on the User model. Authoritative server-side; cookie is a UX-smoothing cache, not the source of truth.
- **Italian source-of-truth:** every IT string currently in JSX is the canonical IT translation. We extract rather than re-translate.

## Architecture decisions

- **Key convention:** namespace by feature, dot-separated. `billing.invoices.received.import.errorImporting`, `users.create.duplicateEmail`, `nav.adminModules`, etc. One JSON tree per locale: `frontend-admin/src/locales/en.json`, `it.json`. Sub-paths mirror the route tree where it makes sense.
- **Typed `t()` from day 1:** generate `Resources` types from `en.json` so misspelled keys fail typecheck. Retrofitting types after thousands of keys exist is painful — pay this cost in Phase 3, not Phase 7.
- **Error-code contract shape:** custom error type that implements `huma.StatusError` and JSON-serializes with a top-level `code` field. One const-per-code registry at `backend/internal/shared/errcode/codes.go`. The `detail` field stays as a human-readable English fallback; admin renders `t(\`errors.${code}\`, { defaultValue: detail })`.
- **Where the language picker lives:** new "Language" select inside the existing user preferences page. Not in `/admin/modules` (that's per-tenant module config, not per-user). Saves via `PATCH /v1/users/me { language }`.
- **No locale routing in URLs.** Operator console is not SEO-indexed; language is a per-user setting, not part of the URL. Saves us from rewriting `react-router` route definitions.
- **Lazy module-by-module extraction.** One PR per backend module's pages keeps reviews tight and lets the error-code refactor for that backend module happen in the same PR.

## Phases

### Phase 0 — Foundations (no code)

1. Decide and document the **key naming convention** in `frontend-admin/CLAUDE.md`. One paragraph + 3 examples.
2. Decide the **error-code naming convention**: `<module>.<situation>`, snake_case (e.g. `auth.email_in_use`, `billing.invoice_not_found`). Add to `backend/CLAUDE.md`.
3. Tag a native Italian speaker as reviewer for Phase 6.

**Exit criteria:** both CLAUDE.md updates merged.

### Phase 1 — Backend: `user.language` field

Smallest possible backend change to persist the preference.

1. **SDK iface first.** Add `Language string \`bson:"language,omitempty" json:"language,omitempty"\`` to the canonical User struct in `github.com/orkestra-cc/orkestra-sdk/iface`. Cut an SDK release.
2. **Bump the SDK** in `backend/go.mod`. The re-export in `backend/internal/core/user/models/user.go` picks it up automatically.
3. **Migration**: backfill `"en"` for all existing users. New idempotent step in `backend/internal/core/user/services/migrations.go` (or wherever user migrations live — confirm).
4. **Endpoint surface**: extend the existing self endpoint — `GET /v1/users/me` returns `language`; `PATCH /v1/users/me` accepts `language` with validation `^(en|it)$`. No new route.
5. **Tests**: handler test for PATCH validation, repo test that migration backfills, snapshot of OpenAPI dump (update `backend/openapi/enterprise.json` per [[project_ci_release_blockers]]).
6. **Tenantscope annotation** on any new query/update calls — see [[project_ci_release_blockers]].

**Exit criteria:** `curl PATCH /v1/users/me {"language":"it"}` then `GET /v1/users/me` returns `language:"it"`. Migration tested against a snapshot of dev Mongo.

### Phase 2 — Backend: error-code contract (lazy, per page)

This phase is **never done independently**. It piggybacks on Phase 4 PRs: when a frontend-admin page in module X is extracted, the same PR refactors module X's admin-facing handlers to return error codes.

**One-time setup (✅ shipped as the Phase 2 setup PR):**

1. ✅ Created `backend/internal/shared/errcode/` (renamed from the planned `errs/` to avoid collisions with stdlib `errors` and the existing `internal/shared/errors/` Manager package). Holds the `Error` envelope type, typed status builders (`BadRequest`, `Unauthorized`, `Forbidden`, `NotFound`, `Conflict`, `UnprocessableEntity`), and `codes.go` with the const registry. First entry: `AuthEmailInUse = "auth.email_in_use"`.
2. ✅ Picked **option A** — a custom error type implementing `huma.StatusError` that JSON-serializes with a top-level `code` field. No global `huma.NewError` override needed; handlers return `*errcode.Error` directly. Wire shape: `{status, title, detail, code}`. Option B was tempting because `admin_user_auth_handler.go` already does the `huma.NewError(status, "code", &huma.ErrorDetail{...})` workaround, but that overloads `detail` with the code string — option A's dedicated `code` field is cleaner and matches the pre-existing local `codedError` in `password_handler.go`, which can later swap to the shared type.
3. ✅ Golden-file contract test (`codes_test.go`): AST-parses `codes.go` and cross-checks every declared const against a snapshot map. Renames, value drift, and forgotten snapshots all fail CI.
4. ✅ Convention documented in `backend/CLAUDE.md` ("Error-code contract" section) with a worked-example code block.

**Per-page (folded into Phase 4 PRs):** every handler that returns an error and is consumed by the admin page being extracted gets a code. Handlers not yet touched stay as-is — the frontend falls back to `detail`.

**Exit criteria for the setup PR (✅):** `POST /v1/users` (and the parallel `POST /v1/admin/client-users` admin-direct create + PATCH `/v1/admin/client-users/{id}` admin update) converted end-to-end. The duplicate-email path now returns `errcode.Conflict(errcode.AuthEmailInUse, "Email already in use")` → `409 {"code":"auth.email_in_use",…}`. Verified against the regenerated OpenAPI dump.

### Phase 3 — `frontend-admin` i18n bootstrap

1. `npm i i18next@^23.16 react-i18next@^15.1 i18next-browser-languagedetector@^8` — match `frontend-client/package.json` versions exactly.
2. Create `frontend-admin/src/i18n.ts` modeled on `frontend-client/src/i18n.ts`. Differences from the client copy:
   - `lng: 'en'`, `fallbackLng: 'en'`.
   - Detection order: `user.language` (custom detector reading from the auth store) → `cookie` → `navigator` → `'en'`.
   - Cookie name `orkestra-admin-lang`, 30-day expiry — distinct from the client cookie so the two SPAs can diverge.
3. Import `./i18n` once in `frontend-admin/src/index.tsx` **before** `<App />`.
4. Add `useLanguageSync()` hook: subscribes to the auth store, calls `i18n.changeLanguage(user.language)` whenever `user.language` changes. Wire into the root component.
5. **Typed keys**: create `frontend-admin/src/i18n-types.d.ts` with:
   ```ts
   import 'react-i18next';
   import en from './locales/en.json';
   declare module 'react-i18next' {
     interface CustomTypeOptions {
       defaultNS: 'translation';
       resources: { translation: typeof en };
     }
   }
   ```
   Add `resolveJsonModule: true` to `tsconfig.json` if not already set.
6. Seed `locales/en.json` and `locales/it.json` with `{ "app": { "name": "Orkestra" } }` only. Render `t('app.name')` somewhere visible to prove the pipeline.
7. Add the language switcher to a dev-only debug panel (or just the new preferences page in Phase 5) so the rest of extraction can be validated in IT mode.

**Exit criteria:** flipping a cookie reloads the app in Italian (with only `app.name` translated). Type errors trigger on misspelled keys.

### Phase 4 — String extraction, module by module

Order chosen by user-visible impact and risk. One PR per item. Each PR also handles the error-code refactor for the backend module(s) the page consumes.

**Order:**

1. **Shared chrome** — top bar, sidebar nav (`Sidebar.tsx`, `NavbarTop*.tsx`), nine-dots menu, notifications dropdown, breadcrumbs, error/empty states, generic `<DataTable />` strings ("No data", "Filter…", pagination labels).
2. **Auth screens** — login, signup, password reset, email verification, MFA setup, OAuth flows. Many of these surface backend errors → first real use of the error-code contract.
3. **`/admin/modules`** + `/admin/modules/:name` — the catalog UI.
4. **`/admin/users`** + user profile.
5. **`/admin/tenant`** + memberships.
6. **`/admin/authz`** + role editor.
7. **`/admin/auth-policy`** (the tabs from [[project_auth_policy_roadmap]]).
8. **`/admin/clients`** (the surface from [[project_admin_clients_management]]).
9. **`/admin/observability`** (log levels) + dashboards links.
10. **Billing** (`/billing/*`) — biggest IT-string concentration (SDI invoice import, FatturaPA flows).
11. **Documents** (`/documents/*`).
12. **Company** (`/company/*`).
13. **Subscriptions + Payments** — Stripe surfaces.
14. **Compliance**, **Identity**, **Marketing**, **Sales**, **Agents/RAG admin**.
15. **Logging** (runtime log-levels page).
16. **Dev module pages** (token issuer).
17. **User preferences page** — must be done before/with Phase 5 so the language picker lives somewhere translated.

**Per-PR checklist:**
- Replace every JSX literal with `t('namespace.key')`.
- Add the key to both `en.json` (literal English) and `it.json` (literal Italian if one was hard-coded; otherwise `"TODO_IT"` marker).
- Refactor the backend handlers this page consumes to return error codes; add `errors.<code>` keys to both locale files.
- Update the page's error-rendering path from `error.message` → `t(\`errors.${error.errorCode}\`, { defaultValue: error.message })`.
- If the backend's OpenAPI changed, regenerate `backend/openapi/enterprise.json`.

**Exit criteria for the phase:** zero raw English/Italian literals in `frontend-admin/src/pages/`. Lint rule from Phase 7 stays green.

### Phase 5 — Language picker in preferences

1. Locate the existing user preferences page (likely `frontend-admin/src/pages/user/Settings.tsx` per the structure described in [[project_user_security_center]] — confirm in the impl PR).
2. Add a "Language" form section with a select: English / Italiano.
3. On change: optimistic `i18n.changeLanguage(value)` → `PATCH /v1/users/me { language: value }` → on failure, revert + toast.
4. Also write to the `orkestra-admin-lang` cookie so the choice survives logout.
5. Visually group with other personal preferences (theme, notification opt-ins).

**Exit criteria:** changing the select instantly re-renders the admin in the chosen language; a fresh login on a different browser inherits the choice from the server.

### Phase 6 — Italian completion pass

1. Grep `it.json` for `TODO_IT` markers — these are strings that had no Italian counterpart in the original JSX.
2. Translate them. Tag the native-speaker reviewer assigned in Phase 0.
3. **Visual smoke test every admin page in IT.** Italian runs ~25% longer than English on average — confirm sidebars, buttons, and table headers don't truncate or wrap badly.
4. Fix layout regressions case by case (`min-width`, `flex-shrink`, abbreviated labels).

**Exit criteria:** zero `TODO_IT` in `it.json`. Manual screenshot review of every top-level admin route in both languages.

### Phase 7 — CI guardrails

1. **Missing-key lint** — small Node script run in CI: fail if any key in `en.json` is missing from `it.json` (or vice versa). Wire into `make ci-frontend-admin`.
2. **Untranslated-literal lint** — enable `react/jsx-no-literals` (or write a small custom ESLint rule) with a curated allowlist (icons, numerics, brand names). Goal: a new hard-coded English string in a PR fails CI.
3. **Type-checked keys** — already on from Phase 3; just confirm `tsc --noEmit` runs in `make ci-frontend-admin`.
4. **Error-code contract** — the golden-file test from Phase 2 already runs in `make ci-backend`. Confirm it does.

**Exit criteria:** all four checks active in `make ci-frontend-admin` and `make ci-backend`.

## Critical files

| Path | Phase | Purpose |
|---|---|---|
| `orkestra-cc/orkestra-sdk/iface/user.go` (external repo) | 1 | Add `Language` field to canonical User |
| `backend/go.mod` | 1 | SDK version bump |
| `backend/internal/core/user/services/migrations.go` | 1 | Backfill `language="en"` |
| `backend/internal/core/user/handlers/user_handler.go` | 1 | Accept/return `language` on `/me` |
| `backend/openapi/enterprise.json` | 1, 4 | Regenerated after route/schema changes |
| `backend/internal/shared/errcode/errcode.go` (new) | 2 | `*errcode.Error` envelope + typed status builders |
| `backend/internal/shared/errcode/codes.go` (new) | 2 | Error code registry |
| `backend/internal/shared/errcode/codes_test.go` (new) | 2 | Golden-file contract test (AST-parses codes.go) |
| `backend/CLAUDE.md` | 0, 2 | Document error-code convention |
| `frontend-admin/package.json` | 3 | Add i18next deps |
| `frontend-admin/src/i18n.ts` (new) | 3 | i18n bootstrap |
| `frontend-admin/src/i18n-types.d.ts` (new) | 3 | Typed `t()` resources |
| `frontend-admin/src/index.tsx` | 3 | Import `./i18n` |
| `frontend-admin/src/locales/en.json` (new) | 3–6 | English strings |
| `frontend-admin/src/locales/it.json` (new) | 3–6 | Italian strings |
| `frontend-admin/src/hooks/useLanguageSync.ts` (new) | 3 | Sync `user.language` → `i18n.changeLanguage` |
| `frontend-admin/src/pages/user/Settings.tsx` (or equivalent) | 5 | Language picker UI |
| `frontend-admin/CLAUDE.md` | 0 | Document key convention |
| `frontend-admin/eslint.config.*` | 7 | Enable `react/jsx-no-literals` |
| `frontend-admin/scripts/check-locales.mjs` (new) | 7 | Missing-key lint script |
| `Makefile` | 7 | Wire lint into `ci-frontend-admin` |

## Explicit non-goals

- **`frontend-client` default-language flip** (currently defaults to IT). Separate decision; not blocked by this plan.
- **Mobile (Flutter) localization.** Flutter has no i18n today; greenfield work, separate plan when prioritized.
- **Notification email template per-locale seeding** beyond `en`. The collection's `locale` column exists; populating IT templates is a follow-up driven by when email matters for IT users.
- **`Accept-Language` middleware → handler context.** Only needed if a non-admin consumer (mobile, public API) starts consuming localized error codes. Admin reads codes, not localized messages, so the header path stays cold.
- **Translation memory / Crowdin / Lokalise integration.** Two languages + a single bilingual reviewer doesn't justify a TMS yet.
- **Right-to-left language support.** Not on the EN+IT path.

## Risks and mitigations

| Risk | Mitigation |
|---|---|
| **Phase 2 (error codes) bleeds into every backend module** and balloons the per-page PR size. | Strictly lazy: a page that doesn't surface backend errors doesn't force its backend module to refactor. Set a per-PR ceiling (e.g. 400 LoC backend changes); split if exceeded. |
| **IT translations diverge in vocabulary** across pages (same concept worded differently because different developers wrote them). | Phase 6 explicitly budgets a vocabulary-alignment pass. Maintain a short glossary in `frontend-admin/src/locales/GLOSSARY.md` updated during extraction. |
| **Italian text overflow** breaks layouts. | Phase 6 screenshot pass. Avoid fixed-width buttons in shared chrome; prefer `min-width` + auto. |
| **Hard-coded strings sneak back in** after Phase 7 lint exists. | Lint rule + PR review. Allowlist must be tight — every new entry needs justification. |
| **SDK release coupling** — Phase 1 needs an SDK cut before backend can bump. | Phase 1 starts with the SDK PR; bundle the SDK release into the same workstream rather than letting it become a queue. |
| **Detector picks the wrong language** before login (cookie absent, browser is IT) and the user sees IT before having a chance to set EN. | Acceptable: matches the cookie-based pattern in `frontend-client`. The picker fixes it permanently after first login. |

## Open questions

1. **Where does the user preferences page live today** in `frontend-admin/src/pages/user/`? Phase 5 confirms when started.
2. **Does Huma v2's error formatter cleanly support adding a top-level `errorCode` field**, or do we fall back to `ErrorDetail`? Decided in the Phase 2 setup PR.
3. **Migration step for the `language` backfill** — is there a registered migration runner in the user module, or do we add a one-shot `init()` check? Confirmed in Phase 1.
4. **Do any addon repos (post-SDK split, see [[project_sdk_split_extractions]]) define their own admin pages** that the lint rule would also need to cover? If yes, extend Phase 7 lint to those repos.

## Sequencing summary

```
Phase 0  ──┐
           ├─► Phase 1 (backend user.language)  ──┐
           │                                       ├─► Phase 3 (i18n bootstrap) ──┐
           └─► Phase 2 setup (error-code shell) ──┘                                │
                                                                                   │
                                Phase 4 (string extraction, per-module PRs)  ◄────┘
                                  │   │   │   ...   │
                                  ▼   ▼   ▼   ...   ▼
                                Phase 5 (picker UI)  ◄── lands once preferences page is in Phase 4 batch
                                  │
                                  ▼
                                Phase 6 (IT completion pass)
                                  │
                                  ▼
                                Phase 7 (CI guardrails — turned on last so they don't block extraction PRs)
```

Phases 0–3 are linear. Phase 4 is the long tail and can be parallelized across contributors once 3 lands. Phase 7 is intentionally last so the lint rule doesn't flag every page mid-extraction.
