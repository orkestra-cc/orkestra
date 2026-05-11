import { useEffect, useState } from 'react';
import { Button, Form, Modal, Spinner } from 'react-bootstrap';
import { toast } from 'react-toastify';
import {
  useCreateBindingMutation,
  useListRolesQuery
} from 'store/api/tenantApi';

interface Props {
  tenantId: string;
  show: boolean;
  onHide: () => void;
}

/**
 * CreateBindingModal grants a role to a user in the current org, with an
 * optional expiry timestamp for contractor/trial access. The TTL index on
 * authz_bindings auto-reaps expired entries.
 */
const CreateBindingModal: React.FC<Props> = ({ tenantId, show, onHide }) => {
  const { data: rolesData, isLoading: rolesLoading } = useListRolesQuery(tenantId, {
    skip: !show
  });
  const [createBinding, { isLoading: isSaving }] = useCreateBindingMutation();

  const [userUUID, setUserUUID] = useState('');
  const [roleId, setRoleId] = useState('');
  const [expiresAt, setExpiresAt] = useState('');

  useEffect(() => {
    if (!show) {
      setUserUUID('');
      setRoleId('');
      setExpiresAt('');
    }
  }, [show]);

  const roles = rolesData?.roles ?? [];
  const canSave = userUUID.trim().length > 0 && roleId.length > 0 && !isSaving;

  const onSave = async () => {
    try {
      await createBinding({
        tenantId,
        body: {
          userUUID: userUUID.trim(),
          roleId,
          expiresAt: expiresAt ? new Date(expiresAt).toISOString() : undefined
        }
      }).unwrap();
      toast.success('Role granted');
      onHide();
    } catch (err: unknown) {
      toast.error('Grant failed: ' + extractError(err));
    }
  };

  return (
    <Modal show={show} onHide={onHide} backdrop="static">
      <Modal.Header closeButton>
        <Modal.Title>Grant role to user</Modal.Title>
      </Modal.Header>
      <Modal.Body>
        <Form>
          <Form.Group className="mb-3">
            <Form.Label>User UUID</Form.Label>
            <Form.Control
              type="text"
              placeholder="019bc6a4-fe55-7cf5-aea7-bfb8142d2b4e"
              value={userUUID}
              onChange={(e) => setUserUUID(e.target.value)}
              autoFocus
            />
            <Form.Text muted>
              The global user UUID (from <code>/v1/admin/users</code> or the user
              profile page). A proper user picker is planned.
            </Form.Text>
          </Form.Group>

          <Form.Group className="mb-3">
            <Form.Label>Role</Form.Label>
            {rolesLoading ? (
              <div>
                <Spinner animation="border" size="sm" /> Loading roles…
              </div>
            ) : (
              <Form.Select
                value={roleId}
                onChange={(e) => setRoleId(e.target.value)}
              >
                <option value="">— Select a role —</option>
                <optgroup label="System roles">
                  {roles
                    .filter((r) => r.isSystem)
                    .map((r) => (
                      <option key={r.id} value={r.id}>
                        {r.name} ({r.permissions.length} perms)
                      </option>
                    ))}
                </optgroup>
                {roles.some((r) => !r.isSystem) && (
                  <optgroup label="Custom roles">
                    {roles
                      .filter((r) => !r.isSystem)
                      .map((r) => (
                        <option key={r.id} value={r.id}>
                          {r.name} ({r.permissions.length} perms)
                        </option>
                      ))}
                  </optgroup>
                )}
              </Form.Select>
            )}
          </Form.Group>

          <Form.Group className="mb-3">
            <Form.Label>Expires at (optional)</Form.Label>
            <Form.Control
              type="datetime-local"
              value={expiresAt}
              onChange={(e) => setExpiresAt(e.target.value)}
            />
            <Form.Text muted>
              Leave empty for permanent grants. Use this for contractor access,
              temporary elevations, or trial memberships.
            </Form.Text>
          </Form.Group>
        </Form>
      </Modal.Body>
      <Modal.Footer>
        <Button variant="secondary" onClick={onHide} disabled={isSaving}>
          Cancel
        </Button>
        <Button variant="primary" onClick={onSave} disabled={!canSave}>
          {isSaving ? <Spinner size="sm" animation="border" className="me-2" /> : null}
          Grant role
        </Button>
      </Modal.Footer>
    </Modal>
  );
};

function extractError(err: unknown): string {
  if (err && typeof err === 'object' && 'data' in err) {
    const data = (err as { data?: { detail?: string; title?: string } }).data;
    return data?.detail || data?.title || 'unknown error';
  }
  return String(err);
}

export default CreateBindingModal;
