// Shared type definitions for the compliance module. Mirrors the shapes
// the backend emits from `GET /v1/admin/audit-events` (see
// backend/internal/addons/compliance/handlers/admin_handler.go).

export type AuditActorType = 'user' | 'system' | 'anonymous';

export type AuditOutcome = 'success' | 'failure' | 'denied';

export interface AuditEvent {
  uuid: string;
  tenantId?: string;
  tenantKind?: string;
  actorUserId?: string;
  actorEmail?: string;
  actorType: AuditActorType;
  action: string;
  resourceType?: string;
  resourceId?: string;
  outcome: AuditOutcome;
  ipAddress?: string;
  userAgent?: string;
  metadata?: Record<string, unknown>;
  timestamp: string;
}

export interface ListAuditEventsParams {
  tenantId?: string;
  actorUserId?: string;
  /** Exact action match, e.g. `auth.login.succeeded` */
  action?: string;
  /** Action family prefix, e.g. `auth.` to match every `auth.*` event */
  actionPrefix?: string;
  resourceType?: string;
  resourceId?: string;
  outcome?: AuditOutcome;
  /** RFC3339 lower bound (inclusive) */
  since?: string;
  /** RFC3339 upper bound (inclusive) */
  until?: string;
  /** 1–500, default 50 */
  limit?: number;
  /** 0-based, default 0 */
  offset?: number;
}

export interface ListAuditEventsResponse {
  items: AuditEvent[];
  total: number;
  limit: number;
  offset: number;
}

// --- SOC2 evidence ---
//
// Mirrors services.Evidence on the backend (compliance/services/soc2.go).
// `controls` is a loose map keyed by the SOC2 control identifier; each
// value is a nested object whose shape depends on the control. `summary`
// surfaces the small handful of scalar counters the UI renders as stat
// cards — stable keys auditors sample against.

export interface Soc2EvidenceSummary {
  privileged_users?: number;
  privileged_with_mfa?: number;
  failed_logins_24h?: number;
  kms_keys_active?: number;
  kms_keys_shredded?: number;
  audit_rows_24h?: number;
  [key: string]: number | undefined;
}

export interface Soc2Evidence {
  generatedAt: string;
  controls: Record<string, unknown>;
  summary: Soc2EvidenceSummary;
}

// --- DSR (data subject rights) ---
//
// Mirrors handlers.ExportOutput / handlers.EraseOutput on the backend
// (compliance/handlers/me_handler.go). A producer is a module that
// contributed personal data to the bundle; the keys in `bundle` match
// the producers' Subject() identifiers.

export interface DsrExportResponse {
  bundle: Record<string, unknown>;
  producers: string[];
  errors?: Record<string, string>;
}

export interface DsrPurgeResult {
  rowsDeleted: number;
  rowsAnonymized: number;
  collections?: string[];
}

export interface DsrEraseResponse {
  purged: Record<string, DsrPurgeResult>;
  totalRows: number;
  errors?: Record<string, string>;
}
