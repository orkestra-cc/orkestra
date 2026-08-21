// Observability types — ADR-0005 Phase F.
// Mirrors backend/internal/core/logging/models.

export type LogLevel = 'debug' | 'info' | 'warn' | 'error';

export const LOG_LEVELS: LogLevel[] = ['debug', 'info', 'warn', 'error'];

export interface AdminModuleEntry {
  name: string;
  effective: LogLevel;
  override?: LogLevel;
  hasOverride: boolean;
}

export interface LogLevelsView {
  global: LogLevel;
  modules: AdminModuleEntry[];
  diagnostics: DiagnosticEntry[];
  logProvider: LogProviderStatus;
  revision: number;
  permanentRevision: number;
  serverTime: string;
  updatedAt: string;
  updatedBy?: string;
}

export interface SetLevelBody {
  level: LogLevel;
}

/** A temporary module threshold reported by the logging workspace. */
export interface DiagnosticEntry {
  module: string;
  level: LogLevel;
  startedAt: string;
  startedBy: string;
  expiresAt?: string;
}

/** Optional log-preview and Grafana-link capabilities for this deployment. */
export interface LogProviderStatus {
  available: boolean;
  grafanaUrl?: string;
}

/** Complete replacement of the permanent configuration, guarded by its durable revision. */
export interface PermanentLogLevelsInput {
  global: LogLevel;
  perModule: Record<string, LogLevel>;
  expectedPermanentRevision: number;
}

export type DiagnosticDurationMinutes = 15 | 60 | 240;

export interface StartDiagnosticInput {
  module: string;
  level: LogLevel;
  durationMinutes?: DiagnosticDurationMinutes;
}

export interface StopDiagnosticInput {
  module: string;
}

/** Minimized server-side projection of one Loki preview event. */
export interface LogEvent {
  timestamp: string;
  level: LogLevel;
  message: string;
  module: string;
  attributes: Record<string, unknown>;
}

export interface LogPreviewResponse {
  events: LogEvent[];
}

export type LogPreviewWindowMinutes = 5 | 15 | 60;

/** Allowlisted filters accepted by the bounded log-preview endpoint. */
export interface LogPreviewFilters {
  module: string;
  windowMinutes: LogPreviewWindowMinutes;
  level?: LogLevel;
  q?: string;
  limit?: number;
}

/** Huma's 409 payload when a permanent edit races with a newer snapshot. */
export interface LogLevelsConflictPayload {
  title?: string;
  status: 409;
  detail: string;
}
