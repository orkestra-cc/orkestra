import { useState } from 'react';
import { Alert, Button, Form, Modal, Spinner } from 'react-bootstrap';
import { toast } from 'react-toastify';
import {
  type Org,
  useAttachOrgMemberAdminMutation,
} from 'store/api/tenantApi';

interface Props {
  org: Org;
  show: boolean;
  onHide: () => void;
}

// Tenant-scoped role names recognized by authz.SeedSystemRoles. Custom
// roles are out of scope for v1 admin-attach — operators wanting a custom
// role still drive that through the Role Management page after the user is
// a member.
const ROLE_OPTIONS = [
  { value: 'org_member', label: 'Member' },
  { value: 'org_viewer', label: 'Viewer (read-only)' },
  { value: 'org_billing', label: 'Billing' },
  { value: 'org_admin', label: 'Admin' },
  { value: 'org_owner', label: 'Owner' },
];

/**
 * AttachMemberModal — operator-side direct grant for tenant memberships.
 * Mirrors the backend POST /v1/admin/tenants/{id}/members handler. Looks
 * up the user by email by default (the common operator workflow); the
 * advanced "by UUID" toggle exists for cases where the email lookup ran
 * against a different audience (operator vs client) and the operator
 * already knows the UUID.
 */
const AttachMemberModal: React.FC<Props> = ({ org, show, onHide }) => {
  const [byUUID, setByUUID] = useState(false);
  const [userEmail, setUserEmail] = useState('');
  const [userUUID, setUserUUID] = useState('');
  const [role, setRole] = useState('org_member');
  const [isOwner, setIsOwner] = useState(false);
  const [errMsg, setErrMsg] = useState<string | null>(null);

  const [attach, { isLoading }] = useAttachOrgMemberAdminMutation();

  const reset = () => {
    setUserEmail('');
    setUserUUID('');
    setRole('org_member');
    setIsOwner(false);
    setByUUID(false);
    setErrMsg(null);
  };

  const handleHide = () => {
    if (isLoading) return;
    reset();
    onHide();
  };

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    setErrMsg(null);

    const body: {
      userUuid?: string;
      userEmail?: string;
      role: string;
      isOwner: boolean;
    } = { role, isOwner };
    if (byUUID) {
      const v = userUUID.trim();
      if (!v) {
        setErrMsg('User UUID is required');
        return;
      }
      body.userUuid = v;
    } else {
      const v = userEmail.trim();
      if (!v) {
        setErrMsg('Email is required');
        return;
      }
      body.userEmail = v;
    }

    try {
      await attach({ tenantId: org.id, body }).unwrap();
      toast.success('Member attached');
      reset();
      onHide();
    } catch (err: unknown) {
      setErrMsg(extractError(err));
    }
  };

  return (
    <Modal show={show} onHide={handleHide} centered>
      <Form onSubmit={handleSubmit}>
        <Modal.Header closeButton>
          <Modal.Title className="fs-9">Attach Member</Modal.Title>
        </Modal.Header>
        <Modal.Body>
          {errMsg && (
            <Alert variant="danger" className="fs-10 py-2">
              {errMsg}
            </Alert>
          )}

          <div className="mb-3 d-flex gap-3 fs-10">
            <Form.Check
              type="radio"
              id="lookup-email"
              label="By email"
              checked={!byUUID}
              onChange={() => setByUUID(false)}
            />
            <Form.Check
              type="radio"
              id="lookup-uuid"
              label="By user UUID"
              checked={byUUID}
              onChange={() => setByUUID(true)}
            />
          </div>

          {byUUID ? (
            <Form.Group className="mb-3">
              <Form.Label className="fs-10">User UUID</Form.Label>
              <Form.Control
                type="text"
                value={userUUID}
                onChange={(e) => setUserUUID(e.target.value)}
                placeholder="e.g. 0192-…"
                autoFocus
              />
              <Form.Text className="text-muted fs-11">
                Use when the user UUID is already known (cross-audience attach,
                copied from another admin tool).
              </Form.Text>
            </Form.Group>
          ) : (
            <Form.Group className="mb-3">
              <Form.Label className="fs-10">User email</Form.Label>
              <Form.Control
                type="email"
                value={userEmail}
                onChange={(e) => setUserEmail(e.target.value)}
                placeholder="user@example.com"
                autoFocus
              />
              <Form.Text className="text-muted fs-11">
                Resolved against the {org.kind === 'external' ? 'client' : 'operator'}
                {' '}user collection (matches this tenant&apos;s tier).
              </Form.Text>
            </Form.Group>
          )}

          <Form.Group className="mb-3">
            <Form.Label className="fs-10">Role</Form.Label>
            <Form.Select value={role} onChange={(e) => setRole(e.target.value)}>
              {ROLE_OPTIONS.map((r) => (
                <option key={r.value} value={r.value}>
                  {r.label} ({r.value})
                </option>
              ))}
            </Form.Select>
          </Form.Group>

          <Form.Group className="mb-1">
            <Form.Check
              type="checkbox"
              id="attach-isowner"
              label="Mark as tenant owner"
              checked={isOwner}
              onChange={(e) => setIsOwner(e.target.checked)}
            />
            <Form.Text className="text-muted fs-11">
              Stamps the denormalized owner flag on the membership row. Does
              not change the tenant&apos;s primary owner record.
            </Form.Text>
          </Form.Group>
        </Modal.Body>
        <Modal.Footer>
          <Button variant="link" onClick={handleHide} disabled={isLoading}>
            Cancel
          </Button>
          <Button type="submit" variant="primary" disabled={isLoading}>
            {isLoading ? (
              <>
                <Spinner animation="border" size="sm" className="me-2" />
                Attaching…
              </>
            ) : (
              'Attach'
            )}
          </Button>
        </Modal.Footer>
      </Form>
    </Modal>
  );
};

function extractError(err: unknown): string {
  if (err && typeof err === 'object' && 'data' in err) {
    const data = (err as { data?: { detail?: string; title?: string } }).data;
    if (data?.detail) return data.detail;
    if (data?.title) return data.title;
  }
  if (err && typeof err === 'object' && 'status' in err) {
    const status = (err as { status?: number | string }).status;
    if (status === 404) return 'User or tenant not found.';
    if (status === 409) return 'User is already a member of this tenant.';
  }
  return 'Attach failed. Check the input and try again.';
}

export default AttachMemberModal;
