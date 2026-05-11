import { useEffect, useState } from 'react';
import { Button, Col, Form, Row } from 'react-bootstrap';
import type { AuditOutcome, ListAuditEventsParams } from 'types/compliance';

interface Props {
  /** Current filter state from the parent page */
  value: ListAuditEventsParams;
  /** Called whenever the user submits the filter form */
  onApply: (next: ListAuditEventsParams) => void;
  /** Called when the user clears filters */
  onReset: () => void;
}

// The action-family vocabulary we surface as a quick prefix filter. Matches
// the families the backend currently emits (see
// backend/internal/addons/compliance/models/audit_event.go).
const ACTION_FAMILIES: { label: string; value: string }[] = [
  { label: 'All families', value: '' },
  { label: 'Authentication (auth.*)', value: 'auth.' },
  { label: 'Tenant lifecycle (tenant.*)', value: 'tenant.' },
  { label: 'Identity / IdP / SCIM (identity.*)', value: 'identity.' },
  { label: 'Subscriptions (subscription.*)', value: 'subscription.' },
  { label: 'Onboarding (onboarding.*)', value: 'onboarding.' },
  { label: 'GDPR DSR (gdpr.*)', value: 'gdpr.' },
];

const OUTCOMES: AuditOutcome[] = ['success', 'failure', 'denied'];

const AuditEventsFilters: React.FC<Props> = ({ value, onApply, onReset }) => {
  const [draft, setDraft] = useState<ListAuditEventsParams>(value);

  // Keep the draft in sync when the parent resets filters programmatically
  // (e.g. on the "Reset" button path or after invalidation). We deliberately
  // ignore changes caused by pagination — those flow through `value` but
  // don't change the user-facing filter fields.
  useEffect(() => {
    setDraft(value);
  }, [value]);

  const handleChange = (patch: Partial<ListAuditEventsParams>) => {
    setDraft((prev) => ({ ...prev, ...patch }));
  };

  const submit = (e: React.FormEvent) => {
    e.preventDefault();
    // Reset offset whenever a filter changes so the user doesn't land on an
    // empty page beyond the new result set.
    onApply({ ...draft, offset: 0 });
  };

  return (
    <Form onSubmit={submit} className="fs-10">
      <Row className="g-2 align-items-end">
        <Col md={6} lg={3}>
          <Form.Label className="mb-1">Action family</Form.Label>
          <Form.Select
            size="sm"
            value={draft.actionPrefix ?? ''}
            onChange={(e) => handleChange({ actionPrefix: e.target.value || undefined })}
          >
            {ACTION_FAMILIES.map((f) => (
              <option key={f.value} value={f.value}>
                {f.label}
              </option>
            ))}
          </Form.Select>
        </Col>
        <Col md={6} lg={3}>
          <Form.Label className="mb-1">Exact action</Form.Label>
          <Form.Control
            size="sm"
            type="text"
            placeholder="auth.login.succeeded"
            value={draft.action ?? ''}
            onChange={(e) => handleChange({ action: e.target.value || undefined })}
          />
        </Col>
        <Col md={6} lg={2}>
          <Form.Label className="mb-1">Outcome</Form.Label>
          <Form.Select
            size="sm"
            value={draft.outcome ?? ''}
            onChange={(e) =>
              handleChange({
                outcome: (e.target.value || undefined) as AuditOutcome | undefined,
              })
            }
          >
            <option value="">Any outcome</option>
            {OUTCOMES.map((o) => (
              <option key={o} value={o}>
                {o}
              </option>
            ))}
          </Form.Select>
        </Col>
        <Col md={6} lg={2}>
          <Form.Label className="mb-1">Tenant ID</Form.Label>
          <Form.Control
            size="sm"
            type="text"
            placeholder="UUID"
            value={draft.tenantId ?? ''}
            onChange={(e) => handleChange({ tenantId: e.target.value || undefined })}
          />
        </Col>
        <Col md={6} lg={2}>
          <Form.Label className="mb-1">Actor user ID</Form.Label>
          <Form.Control
            size="sm"
            type="text"
            placeholder="UUID"
            value={draft.actorUserId ?? ''}
            onChange={(e) => handleChange({ actorUserId: e.target.value || undefined })}
          />
        </Col>

        <Col md={6} lg={3}>
          <Form.Label className="mb-1">Since</Form.Label>
          <Form.Control
            size="sm"
            type="datetime-local"
            value={toDatetimeLocal(draft.since)}
            onChange={(e) =>
              handleChange({ since: fromDatetimeLocal(e.target.value) })
            }
          />
        </Col>
        <Col md={6} lg={3}>
          <Form.Label className="mb-1">Until</Form.Label>
          <Form.Control
            size="sm"
            type="datetime-local"
            value={toDatetimeLocal(draft.until)}
            onChange={(e) =>
              handleChange({ until: fromDatetimeLocal(e.target.value) })
            }
          />
        </Col>
        <Col md={6} lg={2}>
          <Form.Label className="mb-1">Page size</Form.Label>
          <Form.Select
            size="sm"
            value={draft.limit ?? 50}
            onChange={(e) => handleChange({ limit: Number(e.target.value) })}
          >
            <option value={25}>25</option>
            <option value={50}>50</option>
            <option value={100}>100</option>
            <option value={250}>250</option>
            <option value={500}>500</option>
          </Form.Select>
        </Col>
        <Col md={12} lg={4} className="d-flex gap-2">
          <Button type="submit" variant="primary" size="sm" className="flex-grow-1">
            Apply filters
          </Button>
          <Button
            type="button"
            variant="outline-secondary"
            size="sm"
            onClick={() => {
              setDraft({});
              onReset();
            }}
          >
            Reset
          </Button>
        </Col>
      </Row>
    </Form>
  );
};

// datetime-local <-> RFC3339 conversion. The backend wants RFC3339 with a
// timezone offset; the <input type="datetime-local"> control emits a local
// "YYYY-MM-DDTHH:mm" string without an offset. We round-trip via Date so the
// timezone of the user's browser is preserved.
function toDatetimeLocal(iso?: string): string {
  if (!iso) return '';
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return '';
  const pad = (n: number) => String(n).padStart(2, '0');
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}T${pad(
    d.getHours(),
  )}:${pad(d.getMinutes())}`;
}

function fromDatetimeLocal(local: string): string | undefined {
  if (!local) return undefined;
  const d = new Date(local);
  if (Number.isNaN(d.getTime())) return undefined;
  return d.toISOString();
}

export default AuditEventsFilters;
