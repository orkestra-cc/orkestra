import { Alert, Button, Modal, Spinner } from 'react-bootstrap';
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome';
import { toast } from 'react-toastify';
import { useDeleteRoleMutation, type Role } from 'store/api/tenantApi';

interface Props {
  tenantId: string;
  role: Role | null;
  show: boolean;
  onHide: () => void;
}

/**
 * DeleteRoleModal is a typed confirm dialog that replaces the old
 * `window.confirm` flow so the delete cascade warning is visible, the
 * destination role is named inline, and the submit button carries a
 * loading spinner while the PATCH+DELETE round-trip is in flight.
 *
 * System roles never land here — the delete button is hidden for them in
 * RolesTable. We still gate on `role.isSystem` as a defensive noop.
 */
const DeleteRoleModal: React.FC<Props> = ({ tenantId, role, show, onHide }) => {
  const [deleteRole, { isLoading }] = useDeleteRoleMutation();

  const onConfirm = async () => {
    if (!role || role.isSystem) {
      onHide();
      return;
    }
    try {
      await deleteRole({ tenantId, roleId: role.id }).unwrap();
      toast.success(`Role "${role.name}" deleted`);
      onHide();
    } catch (err: unknown) {
      toast.error('Delete failed: ' + extractError(err));
    }
  };

  return (
    <Modal show={show && !!role} onHide={onHide} backdrop="static" centered>
      <Modal.Header closeButton>
        <Modal.Title>
          <FontAwesomeIcon icon="trash" className="text-danger me-2" />
          Delete role
        </Modal.Title>
      </Modal.Header>
      <Modal.Body>
        <p className="mb-3">
          You are about to delete the custom role{' '}
          <code className="fs-9">{role?.name}</code>. This cannot be undone.
        </p>
        <Alert variant="warning" className="fs-10 mb-0">
          <strong>Bindings cascade.</strong> Any user currently granted this
          role will lose its permissions immediately, and the binding rows
          that reference it will be removed.
        </Alert>
      </Modal.Body>
      <Modal.Footer>
        <Button variant="secondary" onClick={onHide} disabled={isLoading}>
          Cancel
        </Button>
        <Button variant="danger" onClick={onConfirm} disabled={isLoading || !role}>
          {isLoading ? (
            <>
              <Spinner size="sm" animation="border" className="me-2" /> Deleting…
            </>
          ) : (
            <>Delete role</>
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

export default DeleteRoleModal;
