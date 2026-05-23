// TypeScript types for the marketing addon — mirror the Go shapes
// in backend/internal/addons/marketing/models/. The source of truth
// for any disagreement is the backend's OpenAPI dump at
// backend/openapi/enterprise.json — these types are hand-written
// because the operator console has not adopted openapi-typescript
// yet, but they should be regenerated when that work lands.

export type OrganizationKind =
  | 'company'
  | 'public_administration'
  | 'foundation'
  | 'association'
  | 'other';

export interface EmailEntry {
  address: string;
  label?: string;
  primary?: boolean;
  verified?: boolean;
  optIn?: boolean;
  optInAt?: string;
  optInSource?: string;
}

export interface PhoneEntry {
  number: string;
  label?: string;
  primary?: boolean;
}

export interface PostalAddress {
  street?: string;
  city?: string;
  province?: string;
  postalCode?: string;
  country?: string;
  label?: string;
  primary?: boolean;
}

export interface ProvenanceSource {
  importer: string;
  jobUuid?: string;
  externalId?: string;
  importedAt: string;
  rawPayloadRef?: string;
}

export interface ConsentRecord {
  given: boolean;
  basis?: 'consent' | 'legitimate_interest';
  givenAt?: string;
  source?: string;
  revokedAt?: string;
}

export interface Consent {
  marketingEmail?: ConsentRecord;
  marketingPhone?: ConsentRecord;
  profiling?: ConsentRecord;
}

// --- Organization ---

export interface Organization {
  uuid: string;
  tenantId: string;
  legalName: string;
  displayName?: string;
  vat?: string;
  taxCode?: string;
  kind: OrganizationKind;
  website?: string;
  emails?: EmailEntry[];
  phones?: PhoneEntry[];
  addresses?: PostalAddress[];
  tags?: string[];
  customFields?: Record<string, unknown>;
  sources?: ProvenanceSource[];
  notes?: string;
  createdAt: string;
  updatedAt: string;
}

export interface OrganizationPayload {
  legalName: string;
  displayName?: string;
  vat?: string;
  taxCode?: string;
  kind?: OrganizationKind;
  website?: string;
  emails?: EmailEntry[];
  phones?: PhoneEntry[];
  addresses?: PostalAddress[];
  tags?: string[];
  customFields?: Record<string, unknown>;
  notes?: string;
}

// --- Person ---

export interface Person {
  uuid: string;
  tenantId: string;
  firstName?: string;
  lastName?: string;
  title?: string;
  emails?: EmailEntry[];
  phones?: PhoneEntry[];
  language?: string;
  birthdate?: string;
  tags?: string[];
  customFields?: Record<string, unknown>;
  consent?: Consent;
  activeCardUuids?: string[];
  sources?: ProvenanceSource[];
  notes?: string;
  createdAt: string;
  updatedAt: string;
}

export interface PersonPayload {
  firstName?: string;
  lastName?: string;
  title?: string;
  emails?: EmailEntry[];
  phones?: PhoneEntry[];
  language?: string;
  birthdate?: string;
  tags?: string[];
  customFields?: Record<string, unknown>;
  consent?: Consent;
  notes?: string;
}

// --- Membership ---

export interface Membership {
  uuid: string;
  tenantId: string;
  personUuid: string;
  orgUuid: string;
  role?: string;
  department?: string;
  since?: string;
  until?: string;
  active: boolean;
  primary: boolean;
  notes?: string;
  createdAt: string;
  updatedAt: string;
}

export interface MembershipPayload {
  orgUuid: string;
  role?: string;
  department?: string;
  since?: string;
  until?: string;
  primary?: boolean;
  notes?: string;
}

// --- Tag ---

export interface Tag {
  uuid: string;
  tenantId: string;
  name: string;
  slug: string;
  description?: string;
  color?: string;
  parentUuid?: string;
  path: string;
  createdAt: string;
  updatedAt: string;
}

export interface TagPayload {
  name: string;
  slug?: string;
  description?: string;
  color?: string;
  parentUuid?: string;
}

// --- Custom field schema ---

export type CustomFieldType =
  | 'string'
  | 'int'
  | 'float'
  | 'bool'
  | 'date'
  | 'datetime'
  | 'enum'
  | 'multi_enum'
  | string; // ref:<collection>

export interface FieldOption {
  value: string;
  label?: string;
}

export interface FieldDef {
  key: string;
  label?: string;
  type: CustomFieldType;
  required?: boolean;
  options?: FieldOption[];
  default?: unknown;
  description?: string;
}

export type CustomFieldTarget = 'persons' | 'organizations';

export interface CustomFieldSchema {
  uuid: string;
  tenantId: string;
  targetCollection: CustomFieldTarget;
  fields: FieldDef[];
  allowUnknownFields: boolean;
  version: number;
  createdAt: string;
  updatedAt: string;
}

export interface CustomFieldSchemaPayload {
  targetCollection: CustomFieldTarget;
  fields: FieldDef[];
  allowUnknownFields?: boolean;
}

// --- Import job ---

// ImportJobStatus mirrors backend models/import_job.go. Phase 3 added
// `paused_for_review` for the conflict-queue branch — the worker
// transitions a job to that state when the first blocking conflict is
// parked, and back to `running` (then `done`) once every pending
// review is resolved or dismissed.
export type ImportJobStatus =
  | 'queued'
  | 'running'
  | 'paused_for_review'
  | 'done'
  | 'failed';

export interface ImportJobStats {
  rowsRead: number;
  rowsFailed?: number;
  orgsCreated?: number;
  orgsMerged?: number;
  personsCreated?: number;
  personsMerged?: number;
  membershipsLinked?: number;
  conflictsSkipped?: number;
  // Phase 4 (PR-4) — engagement-CSV emission counters.
  engagementEmitted?: number;
  engagementOccurredAtFallback?: number;
}

export interface ImportJob {
  uuid: string;
  tenantId: string;
  importer: string;
  sourceName?: string;
  status: ImportJobStatus;
  stats: ImportJobStats;
  error?: string;
  // Phase 3 — idempotency dedup key (sha256 of body + mapping) and
  // back-references to every conflict_review the job parked.
  idempotencyKey?: string;
  conflictReviewUuids?: string[];
  createdAt: string;
  startedAt?: string;
  completedAt?: string;
  createdBy?: string;
}

export interface ColumnMapping {
  columns: Record<string, string>;
  options?: Record<string, string>;
}

// --- Phase 3: conflict-review queue + adapter capabilities ---

// ConflictTargetKind identifies which contact collection the review
// row is attached to.
export type ConflictTargetKind = 'person' | 'organization';

// ConflictReviewStatus tracks the lifecycle of a review.
export type ConflictReviewStatus = 'pending' | 'resolved' | 'dismissed';

// ConflictAction is the operator's choice at close time.
//   keep_existing — discard incoming conflicting fields.
//   take_incoming — overwrite existing with incoming on those fields.
//   manual_merge  — apply fieldOverrides per-key.
//   dismiss       — drop the incoming row entirely.
export type ConflictAction =
  | 'keep_existing'
  | 'take_incoming'
  | 'manual_merge'
  | 'dismiss';

// ConflictSeverity tags a conflict as dedup-key blocking (the row is
// parked pending resolution) or as a soft-match hit (strict-match
// missed but the soft-match helper flagged a similar record).
export type ConflictSeverity = 'blocking' | 'soft';

export interface ConflictField {
  field: string;
  existingValue?: unknown;
  incomingValue?: unknown;
  severity: ConflictSeverity;
}

export interface ConflictResolution {
  action: ConflictAction;
  fieldOverrides?: Record<string, unknown>;
}

export interface ConflictReview {
  uuid: string;
  importJobUuid: string;
  targetKind: ConflictTargetKind;
  existingUuid: string;
  existingSnapshot?: Record<string, unknown>;
  incomingPayload: Record<string, unknown>;
  incomingActivities?: Array<Record<string, unknown>>;
  conflicts: ConflictField[];
  status: ConflictReviewStatus;
  resolution?: ConflictResolution;
  resolvedAt?: string;
  resolvedBy?: string;
  resolvedNotes?: string;
  createdAt: string;
  updatedAt: string;
}

export interface ResolveConflictPayload {
  action: ConflictAction;
  fieldOverrides?: Record<string, unknown>;
  notes?: string;
}

export interface DismissConflictPayload {
  notes?: string;
}

// OdooImportConfig mirrors the backend's importers/odoo/adapter.go
// ImportConfig struct. The wizard's Odoo connection form serialises
// this as the multipart `file` field when the operator picks the
// odoo adapter.
export interface OdooImportConfig {
  baseUrl: string;
  database: string;
  apiKey: string;
  pageSize?: number;
  includeEngagement?: boolean;
  engagementSinceDays?: number;
}

// --- Phase 2: activities + scoring ---

// ActivityKind mirrors backend models/activity_kinds.go. Kept as a
// string union so editors get autocomplete; the backend rejects
// unknown values with 400.
export type ActivityKind =
  // Email
  | 'email_sent'
  | 'email_opened'
  | 'email_clicked'
  | 'email_bounced'
  | 'email_unsubscribed'
  | 'email_complained'
  // Events
  | 'event_invited'
  | 'event_registered'
  | 'event_attended'
  | 'event_no_show'
  | 'event_cancelled'
  // Web / form
  | 'form_submitted'
  | 'page_visited'
  | 'content_downloaded'
  // Direct (the four ManualKinds the POST endpoint accepts)
  | 'call_made'
  | 'meeting_held'
  | 'note_added'
  // System
  | 'imported'
  | 'merged'
  | 'tag_added'
  | 'tag_removed'
  | 'card_issued'
  | 'card_status_changed'
  | 'corrected_by';

// MANUAL_ACTIVITY_KINDS pins the subset the POST /activities surface
// accepts. Anything else returns 400 from the backend.
export const MANUAL_ACTIVITY_KINDS: ActivityKind[] = [
  'call_made',
  'meeting_held',
  'note_added',
  'corrected_by'
];

export type ActivitySource =
  | 'importer'
  | 'campaign_engine'
  | 'webhook'
  | 'manual'
  | 'system';

export interface ActivityRefs {
  campaignUuid?: string;
  eventUuid?: string;
  formUuid?: string;
  contentUuid?: string;
  importJobUuid?: string;
  cardUuid?: string;
  correctsActivityUuid?: string;
}

export interface Activity {
  uuid: string;
  tenantId: string;
  personUuid: string;
  orgUuid?: string;
  kind: ActivityKind;
  occurredAt: string;
  recordedAt: string;
  source: ActivitySource;
  payload?: Record<string, unknown>;
  refs?: ActivityRefs;
  externalId?: string;
  createdBy?: string;
}

export interface ManualActivityPayload {
  personUuid: string;
  kind: ActivityKind;
  occurredAt?: string;
  payload?: Record<string, unknown>;
  refs?: ActivityRefs;
  externalId?: string;
}

export interface CorrectionPayload {
  reason: string;
}

// Decay function shapes — null/undefined window/half-life mean
// the corresponding branch isn't applicable (validated server-side).
export type DecayFnKind = 'none' | 'linear' | 'exponential';

export interface DecayFn {
  fn: DecayFnKind | string;
  windowDays?: number;
  halfLifeDays?: number;
}

// ScoreRule.activityKind is intentionally typed `unknown` so the
// backend's polymorphic shape (string | string[] | "*" wildcard)
// round-trips losslessly through JSON.
export interface ScoreRule {
  activityKind: unknown;
  matchPayload?: Record<string, unknown>;
  points: number;
  decay?: DecayFn;
  cap?: number;
  windowDays?: number;
}

export interface ProfileFilter {
  tagsInclude?: string[];
  tagsExclude?: string[];
  customFieldFilters?: Record<string, unknown>;
}

export interface ScoreProfile {
  uuid: string;
  tenantId: string;
  name: string;
  description?: string;
  active: boolean;
  rules: ScoreRule[];
  filters?: ProfileFilter;
  defaultDecay?: DecayFn;
  version: number;
  createdAt: string;
  updatedAt: string;
  createdBy?: string;
  updatedBy?: string;
}

export interface ScoreProfilePayload {
  name: string;
  description?: string;
  active: boolean;
  rules: ScoreRule[];
  filters?: ProfileFilter;
  defaultDecay?: DecayFn;
}

export interface BreakdownEntry {
  activityUuid: string;
  activityKind: ActivityKind | 'aggregate';
  occurredAt: string;
  ruleIndex: number;
  rawPoints: number;
  appliedDecay: number;
  pointsContributed: number;
}

export interface ScoreSnapshot {
  uuid: string;
  tenantId: string;
  personUuid: string;
  profileUuid: string;
  profileVersion: number;
  value: number;
  breakdown?: BreakdownEntry[];
  asOf: string;
  computedAt: string;
  applicable: boolean;
  stale: boolean;
  activityCount: number;
  lastActivityAt?: string;
}

// LeaderboardEntry is a slimmer projection of ScoreSnapshot (no
// breakdown) — the table cells don't need per-entry detail.
export interface LeaderboardEntry {
  uuid: string;
  personUuid: string;
  profileUuid: string;
  profileVersion: number;
  value: number;
  applicable: boolean;
  stale: boolean;
  activityCount: number;
  lastActivityAt?: string;
  asOf: string;
  computedAt: string;
}

// --- Phase 4: card lifecycle ------------------------------------------

// CardStatus is the lifecycle state of a card instance. Mirrors
// backend models/card_status.go.
//   active    — issued and usable.
//   suspended — temporarily disabled, reversible via reinstate.
//   revoked   — terminal; reinstate is rejected. Also the state the
//               expiration scheduler lands on when expires_at passes.
export type CardStatus = 'active' | 'suspended' | 'revoked';

// CardType is a per-tenant template. Schema mirrors
// backend/internal/addons/marketing/models/card_type.go.
export interface CardType {
  uuid: string;
  tenantId: string;
  key: string; // operator-facing slug, unique per tenant
  displayName: string;
  description?: string;
  codeFormat: string; // grammar in services/card_code_format.go
  tiers?: string[];
  defaultBenefits?: string[];
  allowMultiplePerPerson: boolean;
  active: boolean;
  createdAt: string;
  updatedAt: string;
  createdBy?: string;
  updatedBy?: string;
}

export interface CardTypePayload {
  key: string;
  displayName: string;
  description?: string;
  codeFormat: string;
  tiers?: string[];
  defaultBenefits?: string[];
  allowMultiplePerPerson?: boolean;
  active?: boolean;
}

// Card is a card instance issued to a Person.
export interface Card {
  uuid: string;
  tenantId: string;
  cardTypeUuid: string;
  cardTypeKey?: string;
  personUuid: string;
  code: string;
  tier?: string;
  benefits?: string[];
  status: CardStatus;
  issuedAt: string;
  issuedBy?: string;
  expiresAt?: string;
  suspendedAt?: string;
  suspendedBy?: string;
  suspendReason?: string;
  revokedAt?: string;
  revokedBy?: string;
  revokeReason?: string;
  notes?: string;
  createdAt: string;
  updatedAt: string;
}

export interface IssueCardPayload {
  cardTypeUuid: string;
  tier?: string;
  benefits?: string[];
  expiresAt?: string;
  notes?: string;
}

export interface SuspendCardPayload {
  reason: string;
}

export interface RevokeCardPayload {
  reason: string;
}

// CorrectionEntry mirrors the backend's services/activity_service.go
// CorrectionEntry. Returned by GET /v1/marketing/activities/{id}/corrections.
export interface CorrectionEntry {
  correctingActivityUuid: string;
  recordedAt: string;
  recordedBy?: string;
  reason: string;
}

// --- List envelopes ---

export interface ListMeta {
  limit: number;
  skip: number;
  count: number;
}

export interface PaginatedItems<T> {
  items: T[];
  meta: ListMeta;
}

export interface SimpleItems<T> {
  items: T[];
}
