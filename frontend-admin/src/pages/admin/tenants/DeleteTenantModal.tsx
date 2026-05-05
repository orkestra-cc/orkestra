import { useEffect, useState } from 'react';
import { Alert, Button, Form, Modal, Spinner } from 'react-bootstrap';
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome';
import { toast } from 'react-toastify';
import {
  useDeleteOrgAdminMutation,
  type AdminOrgListItem,
} from 'store/api/tenantApi';

interface Props {
  org: AdminOrgListItem | null;
  show: boolean;
  onHide: () => void;
}

const DeleteTenantModal: React.FC<Props> = ({ org, show, onHide }) => {
  const [deleteOrg, { isLoading }] = useDeleteOrgAdminMutation();
  const [confirmText, setConfirmText] = useState('');

  useEffect(() => {
    if (!show) setConfirmText('');
  }, [show]);

  const canDelete = !!org && confirmText === org.slug && !isLoading;

  const onConfirm = async () => {
    if (!org) return;
    try {
      await deleteOrg(org.id).unwrap();
      toast.success(`Tenant "${org.name}" deleted`);
      onHide();
    } catch (err: unknown) {
      toast.error('Delete failed: ' + extractError(err));
    }
  };

  return (
    <Modal show={show && !!org} onHide={onHide} backdrop="static" centered>
      <Modal.Header closeButton>
        <Modal.Title>
          <FontAwesomeIcon icon="trash" className="text-danger me-2" />
          Delete tenant
        </Modal.Title>
      </Modal.Header>
      <Modal.Body>
        <p className="mb-3">
          You are about to soft-delete tenant{' '}
          <code className="fs-9">{org?.name}</code>. It will stop appearing in
          the default tenant list and no user will be able to access it. You
          can still view it with the "Include soft-deleted" toggle.
        </p>
        <Alert variant="warning" className="fs-10 mb-3">
          <strong>Bindings and memberships remain in place.</strong> If you
          also need to crypto-shred the tenant's encryption key (GDPR
          right-to-erasure), open the tenant's detail modal and use the{' '}
          <em>Purge</em> action instead.
        </Alert>
        <Form.Group>
          <Form.Label className="fw-semibold fs-10">
            Type <code>{org?.slug}</code> to confirm
          </Form.Label>
          <Form.Control
            type="text"
            value={confirmText}
            onChange={(e) => setConfirmText(e.target.value)}
            placeholder={org?.slug}
          />
        </Form.Group>
      </Modal.Body>
      <Modal.Footer>
        <Button variant="secondary" onClick={onHide} disabled={isLoading}>
          Cancel
        </Button>
        <Button variant="danger" onClick={onConfirm} disabled={!canDelete}>
          {isLoading ? (
            <>
              <Spinner size="sm" animation="border" className="me-2" /> Deleting…
            </>
          ) : (
            <>Delete tenant</>
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

export default DeleteTenantModal;
