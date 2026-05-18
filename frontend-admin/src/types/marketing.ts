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

export type ImportJobStatus = 'queued' | 'running' | 'done' | 'failed';

export interface ImportJobStats {
  rowsRead: number;
  rowsFailed?: number;
  orgsCreated?: number;
  orgsMerged?: number;
  personsCreated?: number;
  personsMerged?: number;
  membershipsLinked?: number;
  conflictsSkipped?: number;
}

export interface ImportJob {
  uuid: string;
  tenantId: string;
  importer: string;
  sourceName?: string;
  status: ImportJobStatus;
  stats: ImportJobStats;
  error?: string;
  createdAt: string;
  startedAt?: string;
  completedAt?: string;
  createdBy?: string;
}

export interface ColumnMapping {
  columns: Record<string, string>;
  options?: Record<string, string>;
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
