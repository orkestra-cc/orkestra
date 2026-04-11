import { useEffect, useState } from 'react';
import { Alert, Badge, Button, Form, Modal, Spinner } from 'react-bootstrap';
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome';
import { toast } from 'react-toastify';
import {
  useUpdateRoleMutation,
  type Role,
  type UpdateRoleInput,
} from 'store/api/tenantApi';
import PermissionPicker from './PermissionPicker';

interface Props {
  orgId: string;
  role: Role | null;
  show: boolean;
  onHide: () => void;
}

/**
 * EditRoleModal edits an existing role. Custom roles: every field editable.
 * System roles: only the Active switch writes back — name, description, and
 * permissions are re-seeded from code on every boot so letting operators
 * edit them would be dishonest. Disabling a system role is still allowed so
 * operators can forbid granting, e.g., the developer role.
 *
 * The submit dispatches a PATCH with only the fields that actually changed
 * vs the role snapshot at open time.
 */
const EditRoleModal: React.FC<Props> = ({ orgId, role, show, onHide }) => {
  const [updateRole, { isLoading: isSaving }] = useUpdateRoleMutation();

  const [name, setName] = useState('');
  const [description, setDescription] = useState('');
  const [selected, setSelected] = useState<Set<string>>(new Set());
  const [isActive, setIsActive] = useState(true);

  // Hydrate the form every time the modal opens on a (possibly different)
  // role. Guard on `show` so closing the modal doesn't clobber the snapshot
  // while the closing animation is still running.
  useEffect(() => {
    if (show && role) {
      setName(role.name);
      setDescription(role.description ?? '');
      setSelected(new Set(role.permissions ?? []));
      setIsActive(role.isActive);
    }
  }, [show, role]);

  if (!role) return null;

  const readOnly = role.isSystem;

  // Diff against the role snapshot at open time and only PATCH what changed.
  // Empty diffs close the modal without hitting the network.
  const buildPatch = (): UpdateRoleInput => {
    const patch: UpdateRoleInput = {};
    if (!readOnly) {
      const trimmedName = name.trim();
      if (trimmedName !== role.name) patch.name = trimmedName;
      if (description !== (role.description ?? '')) patch.description = description;
      const nextPerms = Array.from(selected).sort();
      const prevPerms = [...(role.permissions ?? [])].sort();
      if (
        nextPerms.length !== prevPerms.length ||
        nextPerms.some((p, i) => p !== prevPerms[i])
      ) {
        patch.permissions = nextPerms;
      }
    }
    if (isActive !== role.isActive) patch.isActive = isActive;
    return patch;
  };

  const patch = buildPatch();
  const hasChanges = Object.keys(patch).length > 0;
  const canSave =
    !isSaving &&
    hasChanges &&
    (readOnly || (name.trim().length > 0 && selected.size > 0));

  const onSave = async () => {
    if (!hasChanges) {
      onHide();
      return;
    }
    try {
      await updateRole({ orgId, roleId: role.id, body: patch }).unwrap();
      toast.success(`Role "${role.name}" updated`);
      onHide();
    } catch (err: unknown) {
      toast.error('Update failed: ' + extractError(err));
    }
  };

  return (
    <Modal show={show} onHide={onHide} size="lg" backdrop="static" scrollable>
      <Modal.Header closeButton>
        <Modal.Title className="d-flex align-items-center gap-2">
          <FontAwesomeIcon icon="pencil-alt" className="text-primary" />
          Edit role: <code className="fs-9">{role.name}</code>
          {role.isSystem ? (
            <Badge bg="secondary">
              <FontAwesomeIcon icon="lock" className="me-1" />
              system
            </Badge>
          ) : (
            <Badge bg="info">custom</Badge>
          )}
        </Modal.Title>
      </Modal.Header>
      <Modal.Body>
        {readOnly && (
          <Alert variant="info" className="mb-3 fs-10">
            <FontAwesomeIcon icon="info-circle" className="me-2" />
            System roles come from code. Their name, description, and
            permissions are re-seeded on every boot, so they're read-only
            here. You can still disable this role to prevent it from being
            granted to users.
          </Alert>
        )}

        {/* Active switch — highlighted as the primary control */}
        <div className="bg-body-tertiary border rounded p-3 mb-4">
          <Form.Check
            type="switch"
            id="role-active-switch"
            className="m-0"
            checked={isActive}
            onChange={(e) => setIsActive(e.target.checked)}
            label={
              <span>
                <strong className="d-block">Active</strong>
                <span className="text-muted small">
                  When off, existing bindings stop granting this role's
                  permissions and new bindings cannot be created.
                </span>
              </span>
            }
          />
        </div>

        <Form>
          <div className="row g-3 mb-4">
            <Form.Group className="col-md-5">
              <Form.Label className="fw-semibold">Name</Form.Label>
              <Form.Control
                type="text"
                value={name}
                onChange={(e) => setName(e.target.value)}
                maxLength={80}
                disabled={readOnly}
              />
            </Form.Group>
            <Form.Group className="col-md-7">
              <Form.Label className="fw-semibold">Description</Form.Label>
              <Form.Control
                type="text"
                value={description}
                onChange={(e) => setDescription(e.target.value)}
                disabled={readOnly}
              />
            </Form.Group>
          </div>

          <PermissionPicker
            selected={selected}
            onChange={setSelected}
            readOnly={readOnly}
          />
        </Form>
      </Modal.Body>
      <Modal.Footer>
        <Button variant="secondary" onClick={onHide} disabled={isSaving}>
          Cancel
        </Button>
        <Button variant="primary" onClick={onSave} disabled={!canSave}>
          {isSaving ? (
            <>
              <Spinner size="sm" animation="border" className="me-2" /> Saving…
            </>
          ) : (
            <>Save changes</>
          )}
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

export default EditRoleModal;
