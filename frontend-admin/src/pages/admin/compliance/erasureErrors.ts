import type { TFunction } from 'i18next';
import { formatDate } from './complianceFormat';

// Backend error codes the erasure workflow can answer with. Each has a
// translated message under adminCompliance.erasure.errors; the body members
// (have/need, retainUntil, requestId) are interpolated.
const KNOWN = new Set([
  'compliance.erasure_approvals_missing',
  'compliance.erasure_self_approval',
  'compliance.erasure_already_approved',
  'compliance.erasure_mode_mismatch',
  'compliance.erasure_request_exists',
  'compliance.erasure_request_busy',
  'compliance.erasure_request_started',
  'compliance.erasure_request_not_closable',
  'compliance.erasure_close_note_required',
  'compliance.legal_hold_active',
  'compliance.retention_minimum_not_met',
  'compliance.retention_check_unavailable',
  'compliance.reason_required',
  'compliance.invalid_cursor',
  'compliance.erasure_receipt_unavailable',
  'compliance.legal_hold_expiry_past',
  'compliance.export_storage_unavailable',
  'compliance.export_erasure_in_progress',
  'compliance.export_expired',
  'compliance.export_not_found',
  'compliance.subject_not_found'
]);

interface ErrBody {
  code?: string;
  have?: number;
  need?: number;
  requestId?: string;
  retainUntil?: string;
  reasons?: string[];
}

// erasureErrorMessage maps an RTK Query error to a translated message, or to
// the given fallback when the code is not one the workflow knows.
export const erasureErrorMessage = (
  err: unknown,
  t: TFunction,
  fallback: string
): string => {
  const body = (err as { data?: ErrBody } | null)?.data;
  const code = body?.code;
  if (!code || !KNOWN.has(code)) return fallback;
  return t(
    `adminCompliance.erasure.errors.${code.replace('compliance.', '')}`,
    {
      have: body.have,
      need: body.need,
      requestId: body.requestId,
      date: body.retainUntil ? formatDate(body.retainUntil) : '',
      reasons: (body.reasons ?? [])
        .map(r =>
          t(`adminCompliance.erasure.retentionReason.${r}`, { defaultValue: r })
        )
        .join(', ')
    }
  );
};
