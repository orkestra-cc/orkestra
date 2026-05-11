import { useEffect, useState } from 'react';
import { Alert, Button, Form, Modal, Spinner } from 'react-bootstrap';
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome';
import { toast } from 'react-toastify';
import {
  usePurgeOrgAdminMutation,
  type AdminOrgListItem,
} from 'store/api/tenantApi';

interface Props {
  org: AdminOrgListItem | null;
  show: boolean;
  onHide: () => void;
  /** Called after the purge request succeeds, so the parent can close any
   *  upstream modals or refresh its own state. */
  onPurged?: () => void;
}

// Purge is the terminal, irreversible lifecycle transition. The backend
// flips status to `purged` AND crypto-shreds the tenant's KMS key — every
// ciphertext sealed with that key becomes mathematically unrecoverable.
// The modal implements a double-confirm: the operator must read the
// warning AND type the tenant slug verbatim before the confirm button
// lights up.
type Stage = 'warn' | 'type-slug';

const PurgeTenantModal: React.FC<Props> = ({ org, show, onHide, onPurged }) => {
  const [purgeOrg, { isLoading }] = usePurgeOrgAdminMutation();
  const [stage, setStage] = useState<Stage>('warn');
  const [typed, setTyped] = useState('');

  useEffect(() => {
    if (show) {
      setStage('warn');
      setTyped('');
    }
  }, [show]);

  if (!org) return null;

  const slugMatches = typed.trim() === org.slug;

  const handleClose = () => {
    if (isLoading) return;
    onHide();
  };

  const onConfirm = async () => {
    try {
      await purgeOrg(org.id).unwrap();
      toast.success(`Tenant "${org.name}" purged — KMS key shredded.`);
      onPurged?.();
      onHide();
    } catch (err: unknown) {
      toast.error('Purge failed: ' + extractError(err));
    }
  };

  return (
    <Modal
      show={show}
      onHide={handleClose}
      centered
      backdrop={isLoading ? 'static' : true}
    >
      <Modal.Header closeButton={!isLoading} className="bg-danger-subtle">
        <Modal.Title className="fs-8 text-danger">
          <FontAwesomeIcon icon="exclamation-triangle" className="me-2" />
          Purge tenant
        </Modal.Title>
      </Modal.Header>

      {stage === 'warn' && (
        <>
          <Modal.Body className="fs-10">
            <Alert variant="danger" className="fs-10 mb-3">
              <strong>Irreversible — cryptographically unrecoverable.</strong>
              <br />
              Purging crypto-shreds the tenant's KMS key. Every ciphertext sealed with
              that key becomes mathematically unrecoverable even if the rows themselves
              are restored from a backup.
            </Alert>
            <p className="mb-2">
              You are about to purge tenant{' '}
              <code className="fs-9">{org.name}</code>{' '}
              <span className="text-body-tertiary">
                (<code className="fs-11">{org.slug}</code>)
              </span>
              .
            </p>
            <ul className="mb-0">
              <li>Tenant status flips to <code>purged</code>.</li>
              <li>The wrapped data-encryption key is deleted from KMS.</li>
              <li>No undo, no grace period, no recovery path.</li>
              <li>
                Audit-trail rows referencing the tenant are retained for regulatory
                reasons; the data they describe stays sealed.
              </li>
            </ul>
          </Modal.Body>
          <Modal.Footer>
            <Button variant="outline-secondary" onClick={handleClose}>
              Cancel
            </Button>
            <Button variant="danger" onClick={() => setStage('type-slug')}>
              I understand — continue
            </Button>
          </Modal.Footer>
        </>
      )}

      {stage === 'type-slug' && (
        <>
          <Modal.Body className="fs-10">
            <p className="mb-2">
              Type the tenant slug below to enable the purge button:
            </p>
            <p className="fs-11 text-body-tertiary mb-3">
              <code>{org.slug}</code>
            </p>
            <Form.Control
              autoFocus
              type="text"
              value={typed}
              onChange={(e) => setTyped(e.target.value)}
              placeholder={org.slug}
              isInvalid={typed.length > 0 && !slugMatches}
              isValid={slugMatches}
            />
            <Form.Control.Feedback type="invalid">
              Slug does not match.
            </Form.Control.Feedback>
          </Modal.Body>
          <Modal.Footer>
            <Button
              variant="outline-secondary"
              onClick={handleClose}
              disabled={isLoading}
            >
              Cancel
            </Button>
            <Button
              variant="danger"
              onClick={onConfirm}
              disabled={!slugMatches || isLoading}
            >
              {isLoading ? (
                <>
                  <Spinner animation="border" size="sm" className="me-2" />
                  Purging…
                </>
              ) : (
                <>Purge tenant</>
              )}
            </Button>
          </Modal.Footer>
        </>
      )}
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

export default PurgeTenantModal;
