import { Button, Modal } from 'react-bootstrap';
import SubtleBadge from 'components/common/SubtleBadge';
import type { BadgeColor } from 'components/common/SubtleBadge';
import type { AuditEvent, AuditOutcome } from 'types/compliance';

interface Props {
  event: AuditEvent | null;
  show: boolean;
  onHide: () => void;
}

const outcomeColor: Record<AuditOutcome, BadgeColor> = {
  success: 'success',
  failure: 'danger',
  denied: 'warning',
};

// The modal surfaces the raw audit event so operators can verify the
// metadata a caller emitted without dropping into the Mongo shell. Read-only
// by design — audit events are append-only on the backend.
const AuditEventDetailModal: React.FC<Props> = ({ event, show, onHide }) => {
  if (!event) return null;
  return (
    <Modal show={show} onHide={onHide} size="lg" centered>
      <Modal.Header closeButton>
        <Modal.Title className="fs-8">
          <code>{event.action}</code>{' '}
          <SubtleBadge bg={outcomeColor[event.outcome]} pill className="ms-2">
            {event.outcome}
          </SubtleBadge>
        </Modal.Title>
      </Modal.Header>
      <Modal.Body className="fs-10">
        <dl className="row mb-0">
          <dt className="col-sm-3 text-body-secondary">UUID</dt>
          <dd className="col-sm-9">
            <code className="fs-11">{event.uuid}</code>
          </dd>

          <dt className="col-sm-3 text-body-secondary">Timestamp</dt>
          <dd className="col-sm-9">{new Date(event.timestamp).toISOString()}</dd>

          <dt className="col-sm-3 text-body-secondary">Actor</dt>
          <dd className="col-sm-9">
            <SubtleBadge bg="info" pill className="me-2">
              {event.actorType}
            </SubtleBadge>
            {event.actorEmail && <span className="me-2">{event.actorEmail}</span>}
            {event.actorUserId && (
              <code className="fs-11 text-body-tertiary">{event.actorUserId}</code>
            )}
            {!event.actorEmail && !event.actorUserId && (
              <span className="text-body-tertiary">—</span>
            )}
          </dd>

          <dt className="col-sm-3 text-body-secondary">Tenant</dt>
          <dd className="col-sm-9">
            {event.tenantId ? (
              <>
                <code className="fs-11">{event.tenantId}</code>
                {event.tenantKind && (
                  <span className="text-body-tertiary ms-2">({event.tenantKind})</span>
                )}
              </>
            ) : (
              <span className="text-body-tertiary">—</span>
            )}
          </dd>

          <dt className="col-sm-3 text-body-secondary">Resource</dt>
          <dd className="col-sm-9">
            {event.resourceType ? (
              <>
                <span className="me-2">{event.resourceType}</span>
                {event.resourceId && (
                  <code className="fs-11 text-body-tertiary">{event.resourceId}</code>
                )}
              </>
            ) : (
              <span className="text-body-tertiary">—</span>
            )}
          </dd>

          <dt className="col-sm-3 text-body-secondary">IP / UA</dt>
          <dd className="col-sm-9 text-body-tertiary fs-11">
            {event.ipAddress ?? '—'}
            {event.userAgent ? ` · ${event.userAgent}` : ''}
          </dd>

          <dt className="col-sm-3 text-body-secondary">Metadata</dt>
          <dd className="col-sm-9">
            {event.metadata && Object.keys(event.metadata).length > 0 ? (
              <pre
                className="bg-body-tertiary rounded p-2 mb-0 fs-11"
                style={{ maxHeight: 320, overflow: 'auto' }}
              >
                {JSON.stringify(event.metadata, null, 2)}
              </pre>
            ) : (
              <span className="text-body-tertiary">—</span>
            )}
          </dd>
        </dl>
      </Modal.Body>
      <Modal.Footer>
        <Button variant="outline-secondary" size="sm" onClick={onHide}>
          Close
        </Button>
      </Modal.Footer>
    </Modal>
  );
};

export default AuditEventDetailModal;
