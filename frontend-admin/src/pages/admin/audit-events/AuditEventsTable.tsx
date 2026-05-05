import { Card, Spinner, Table } from 'react-bootstrap';
import SubtleBadge from 'components/common/SubtleBadge';
import type { BadgeColor } from 'components/common/SubtleBadge';
import type { AuditEvent, AuditOutcome } from 'types/compliance';

interface Props {
  events: AuditEvent[];
  isLoading: boolean;
  error: boolean;
  onRowClick: (ev: AuditEvent) => void;
}

const outcomeColor: Record<AuditOutcome, BadgeColor> = {
  success: 'success',
  failure: 'danger',
  denied: 'warning',
};

const actorColor: Record<string, BadgeColor> = {
  user: 'info',
  system: 'secondary',
  anonymous: 'light',
};

const AuditEventsTable: React.FC<Props> = ({ events, isLoading, error, onRowClick }) => {
  if (error) {
    return (
      <Card>
        <Card.Body className="text-center text-danger py-5">
          Failed to load audit events. You need the{' '}
          <code>system.compliance.audit.read</code> permission to view this page.
        </Card.Body>
      </Card>
    );
  }

  return (
    <Card>
      <Card.Body className="p-0">
        {isLoading ? (
          <div className="text-center py-5">
            <Spinner animation="border" size="sm" />
          </div>
        ) : (
          <Table responsive size="sm" className="fs-10 mb-0">
            <thead className="bg-body-tertiary">
              <tr>
                <th className="ps-3">Timestamp</th>
                <th>Action</th>
                <th>Actor</th>
                <th>Resource</th>
                <th>Tenant</th>
                <th>Outcome</th>
                <th className="pe-3">Metadata</th>
              </tr>
            </thead>
            <tbody>
              {events.map((ev) => (
                <tr
                  key={ev.uuid}
                  className="align-middle"
                  style={{ cursor: 'pointer' }}
                  onClick={() => onRowClick(ev)}
                >
                  <td className="ps-3 text-nowrap text-body-secondary">
                    {formatTimestamp(ev.timestamp)}
                  </td>
                  <td>
                    <code className="fs-11">{ev.action}</code>
                  </td>
                  <td>
                    <div className="d-flex flex-column">
                      <SubtleBadge
                        bg={actorColor[ev.actorType] ?? 'secondary'}
                        pill
                        className="align-self-start mb-1"
                      >
                        {ev.actorType}
                      </SubtleBadge>
                      {ev.actorEmail && (
                        <span className="fs-11 text-body-secondary">
                          {ev.actorEmail}
                        </span>
                      )}
                      {!ev.actorEmail && ev.actorUserId && (
                        <code className="fs-11 text-body-tertiary">
                          {shorten(ev.actorUserId)}
                        </code>
                      )}
                    </div>
                  </td>
                  <td>
                    {ev.resourceType ? (
                      <div className="d-flex flex-column">
                        <span className="fs-11">{ev.resourceType}</span>
                        {ev.resourceId && (
                          <code className="fs-11 text-body-tertiary">
                            {shorten(ev.resourceId)}
                          </code>
                        )}
                      </div>
                    ) : (
                      <span className="text-body-tertiary">—</span>
                    )}
                  </td>
                  <td>
                    {ev.tenantId ? (
                      <code className="fs-11 text-body-tertiary">
                        {shorten(ev.tenantId)}
                      </code>
                    ) : (
                      <span className="text-body-tertiary">—</span>
                    )}
                  </td>
                  <td>
                    <SubtleBadge bg={outcomeColor[ev.outcome]} pill>
                      {ev.outcome}
                    </SubtleBadge>
                  </td>
                  <td className="pe-3 text-body-tertiary fs-11">
                    {metadataPreview(ev.metadata)}
                  </td>
                </tr>
              ))}
              {events.length === 0 && (
                <tr>
                  <td colSpan={7} className="text-center text-muted py-4">
                    No audit events match the current filters.
                  </td>
                </tr>
              )}
            </tbody>
          </Table>
        )}
      </Card.Body>
    </Card>
  );
};

function formatTimestamp(iso: string): string {
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return iso;
  return d.toLocaleString(undefined, {
    year: 'numeric',
    month: 'short',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
    second: '2-digit',
  });
}

function shorten(id: string): string {
  if (id.length <= 10) return id;
  return `${id.slice(0, 8)}…`;
}

function metadataPreview(metadata?: Record<string, unknown>): string {
  if (!metadata) return '—';
  const entries = Object.entries(metadata);
  if (entries.length === 0) return '—';
  // Show at most the first two scalar entries so the row stays single-line.
  const preview = entries
    .slice(0, 2)
    .map(([k, v]) => `${k}=${formatScalar(v)}`)
    .join(' · ');
  return entries.length > 2 ? `${preview} · …` : preview;
}

function formatScalar(v: unknown): string {
  if (v === null || v === undefined) return '∅';
  if (typeof v === 'string') {
    return v.length > 24 ? `${v.slice(0, 24)}…` : v;
  }
  if (typeof v === 'object') return '{…}';
  return String(v);
}

export default AuditEventsTable;
