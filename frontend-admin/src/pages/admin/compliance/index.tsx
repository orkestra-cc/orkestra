import { useState, type FormEvent } from 'react';
import {
  Badge,
  Button,
  Card,
  Form,
  Spinner,
  Tab,
  Table,
  Tabs
} from 'react-bootstrap';
import { toast } from 'react-toastify';
import {
  useExecuteErasureRequestMutation,
  useListAuditEventsQuery,
  useListErasureRequestsQuery,
  useListLegalHoldsQuery,
  usePlaceLegalHoldMutation,
  useRejectErasureRequestMutation,
  useReleaseLegalHoldMutation,
  useRetentionPreviewQuery
} from 'store/api/complianceApi';

// CompliancePage is the operator-facing GDPR/compliance dashboard (ADR-0009):
// review and resolve erasure requests, manage legal holds, preview retention
// cleanup, and read the audit trail. Destructive actions are step-up-gated on
// the backend — the global StepUpModal handles the 401 + replay transparently.

const ErasureRequestsTab = () => {
  const { data, isLoading } = useListErasureRequestsQuery();
  const [execute] = useExecuteErasureRequestMutation();
  const [reject] = useRejectErasureRequestMutation();

  const onExecute = async (id: string) => {
    try {
      await execute({ id, mode: 'hard_delete' }).unwrap();
      toast.success('Erasure executed');
    } catch {
      toast.error('Execute failed (or blocked by a legal hold)');
    }
  };
  const onReject = async (id: string) => {
    try {
      await reject({ id }).unwrap();
      toast.success('Request rejected');
    } catch {
      toast.error('Reject failed');
    }
  };

  if (isLoading) return <Spinner animation="border" size="sm" />;
  const items = data?.items ?? [];
  if (items.length === 0)
    return <p className="text-muted mb-0">No pending erasure requests.</p>;

  return (
    <Table responsive size="sm" className="mb-0">
      <thead>
        <tr>
          <th>Subject</th>
          <th>Reason</th>
          <th>Requested</th>
          <th className="text-end">Actions</th>
        </tr>
      </thead>
      <tbody>
        {items.map(r => (
          <tr key={r.uuid}>
            <td className="font-monospace small">{r.userUuid}</td>
            <td>{r.reason || '—'}</td>
            <td>{new Date(r.requestedAt).toLocaleString()}</td>
            <td className="text-end">
              <Button
                size="sm"
                variant="outline-danger"
                className="me-2"
                onClick={() => onExecute(r.uuid)}
              >
                Execute
              </Button>
              <Button
                size="sm"
                variant="outline-secondary"
                onClick={() => onReject(r.uuid)}
              >
                Reject
              </Button>
            </td>
          </tr>
        ))}
      </tbody>
    </Table>
  );
};

const LegalHoldsTab = () => {
  const { data, isLoading } = useListLegalHoldsQuery();
  const [place] = usePlaceLegalHoldMutation();
  const [release] = useReleaseLegalHoldMutation();
  const [userUuid, setUserUuid] = useState('');
  const [reason, setReason] = useState('');

  const onPlace = async (e: FormEvent) => {
    e.preventDefault();
    if (!userUuid || !reason) return;
    try {
      await place({ userUuid, reason }).unwrap();
      toast.success('Legal hold placed');
      setUserUuid('');
      setReason('');
    } catch {
      toast.error('Place failed');
    }
  };
  const onRelease = async (id: string) => {
    try {
      await release({
        id,
        releaseReason: 'released via admin console'
      }).unwrap();
      toast.success('Legal hold released');
    } catch {
      toast.error('Release failed');
    }
  };

  const items = data?.items ?? [];
  return (
    <>
      <Form className="d-flex gap-2 mb-3" onSubmit={onPlace}>
        <Form.Control
          size="sm"
          placeholder="Subject userUuid"
          value={userUuid}
          onChange={e => setUserUuid(e.target.value)}
        />
        <Form.Control
          size="sm"
          placeholder="Reason"
          value={reason}
          onChange={e => setReason(e.target.value)}
        />
        <Button
          size="sm"
          type="submit"
          variant="primary"
          className="text-nowrap"
        >
          Place hold
        </Button>
      </Form>
      {isLoading ? (
        <Spinner animation="border" size="sm" />
      ) : items.length === 0 ? (
        <p className="text-muted mb-0">No active legal holds.</p>
      ) : (
        <Table responsive size="sm" className="mb-0">
          <thead>
            <tr>
              <th>Subject</th>
              <th>Reason</th>
              <th>Case</th>
              <th>Placed</th>
              <th className="text-end">Actions</th>
            </tr>
          </thead>
          <tbody>
            {items.map(h => (
              <tr key={h.uuid}>
                <td className="font-monospace small">{h.userUuid}</td>
                <td>{h.reason}</td>
                <td>{h.caseRef || '—'}</td>
                <td>{new Date(h.placedAt).toLocaleString()}</td>
                <td className="text-end">
                  <Button
                    size="sm"
                    variant="outline-secondary"
                    onClick={() => onRelease(h.uuid)}
                  >
                    Release
                  </Button>
                </td>
              </tr>
            ))}
          </tbody>
        </Table>
      )}
    </>
  );
};

const RetentionTab = () => {
  const { data, isLoading } = useRetentionPreviewQuery();
  if (isLoading) return <Spinner animation="border" size="sm" />;
  return (
    <>
      <p className="mb-2">
        Anonymized tombstones past the retention window that auto-cleanup would
        hard-delete (dry run — nothing is deleted here).
      </p>
      <p className="text-muted small">
        Cutoff: {data ? new Date(data.cutoff).toLocaleString() : '—'} ·
        Candidates: <Badge bg="secondary">{data?.count ?? 0}</Badge>
      </p>
      {data && data.count > 0 && (
        <ul className="small font-monospace">
          {data.userUuids.map(u => (
            <li key={u}>{u}</li>
          ))}
        </ul>
      )}
    </>
  );
};

const AuditEventsTab = () => {
  const { data, isLoading } = useListAuditEventsQuery({ limit: 50 });
  if (isLoading) return <Spinner animation="border" size="sm" />;
  const items = data?.items ?? [];
  if (items.length === 0)
    return <p className="text-muted mb-0">No audit events.</p>;
  return (
    <Table responsive size="sm" className="mb-0">
      <thead>
        <tr>
          <th>Time</th>
          <th>Action</th>
          <th>Actor</th>
          <th>Resource</th>
          <th>Outcome</th>
        </tr>
      </thead>
      <tbody>
        {items.map(e => (
          <tr key={e.uuid}>
            <td>{new Date(e.timestamp).toLocaleString()}</td>
            <td className="font-monospace small">{e.action}</td>
            <td className="small">
              {e.actorEmail || e.actorUserId || e.actorType}
            </td>
            <td className="small">
              {e.resourceType}
              {e.resourceId ? `/${e.resourceId}` : ''}
            </td>
            <td>
              <Badge
                bg={
                  e.outcome === 'success'
                    ? 'success'
                    : e.outcome === 'denied'
                      ? 'warning'
                      : 'danger'
                }
              >
                {e.outcome}
              </Badge>
            </td>
          </tr>
        ))}
      </tbody>
    </Table>
  );
};

const CompliancePage = () => (
  <>
    <div className="mb-3">
      <h4 className="mb-1">Compliance</h4>
      <p className="text-muted mb-0">
        Audit trail &amp; GDPR data-subject rights (erasure requests, legal
        holds, retention).
      </p>
    </div>
    <Card>
      <Card.Body>
        <Tabs defaultActiveKey="requests" className="mb-3">
          <Tab eventKey="requests" title="Erasure Requests">
            <ErasureRequestsTab />
          </Tab>
          <Tab eventKey="holds" title="Legal Holds">
            <LegalHoldsTab />
          </Tab>
          <Tab eventKey="retention" title="Retention">
            <RetentionTab />
          </Tab>
          <Tab eventKey="audit" title="Audit Events">
            <AuditEventsTab />
          </Tab>
        </Tabs>
      </Card.Body>
    </Card>
  </>
);

export default CompliancePage;
