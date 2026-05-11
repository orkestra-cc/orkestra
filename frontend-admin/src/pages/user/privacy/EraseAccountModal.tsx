import { useEffect, useState } from 'react';
import { Button, Form, Modal, Spinner } from 'react-bootstrap';
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome';

interface Props {
  show: boolean;
  onHide: () => void;
  onConfirm: () => Promise<void> | void;
  userEmail: string;
  isProcessing: boolean;
}

// Triple-confirm erase flow. Three distinct states so there is no single
// slip that can wipe a user's data — the modal deliberately gets more
// hostile-looking with each step to disrupt muscle memory.
type Stage = 'warn' | 'type-email' | 'final';

const EraseAccountModal: React.FC<Props> = ({
  show,
  onHide,
  onConfirm,
  userEmail,
  isProcessing,
}) => {
  const [stage, setStage] = useState<Stage>('warn');
  const [typed, setTyped] = useState('');

  useEffect(() => {
    if (show) {
      setStage('warn');
      setTyped('');
    }
  }, [show]);

  const emailMatches = typed.trim().toLowerCase() === userEmail.trim().toLowerCase();

  const handleClose = () => {
    if (isProcessing) return;
    onHide();
  };

  return (
    <Modal show={show} onHide={handleClose} centered backdrop={isProcessing ? 'static' : true}>
      <Modal.Header closeButton={!isProcessing} className="bg-danger-subtle">
        <Modal.Title className="fs-8 text-danger">
          <FontAwesomeIcon icon="trash" className="me-2" />
          Delete your account
        </Modal.Title>
      </Modal.Header>

      {stage === 'warn' && (
        <>
          <Modal.Body className="fs-10">
            <p className="mb-3">
              You are about to <strong>permanently delete</strong> your account
              and every record tied to it. This is a GDPR right-to-erasure
              action — the backend runs every registered PII producer and
              hard-deletes the matching rows.
            </p>
            <ul className="mb-3">
              <li>Your user profile, sessions, MFA factors, and OAuth bindings are wiped.</li>
              <li>All refresh tokens are invalidated — you cannot sign back in.</li>
              <li>
                Data you authored <em>about</em> other subjects (shared notes,
                audit rows, workspace edits) is retained for regulatory and
                audit-trail reasons.
              </li>
              <li>There is no undo, no grace period, and no recovery path.</li>
            </ul>
            <p className="mb-0 text-body-secondary">
              If you only want to stop receiving notifications, use{' '}
              <strong>Account&nbsp;→&nbsp;Notifications</strong> instead.
            </p>
          </Modal.Body>
          <Modal.Footer>
            <Button variant="outline-secondary" onClick={handleClose}>
              Cancel
            </Button>
            <Button variant="danger" onClick={() => setStage('type-email')}>
              Continue
            </Button>
          </Modal.Footer>
        </>
      )}

      {stage === 'type-email' && (
        <>
          <Modal.Body className="fs-10">
            <p className="mb-2">
              Type your email below to confirm the account you are deleting:
            </p>
            <p className="fs-11 text-body-tertiary mb-3">
              <code>{userEmail}</code>
            </p>
            <Form.Control
              autoFocus
              type="email"
              value={typed}
              onChange={(e) => setTyped(e.target.value)}
              placeholder="you@example.com"
              isInvalid={typed.length > 0 && !emailMatches}
            />
            <Form.Control.Feedback type="invalid">
              Email does not match.
            </Form.Control.Feedback>
          </Modal.Body>
          <Modal.Footer>
            <Button variant="outline-secondary" onClick={handleClose}>
              Cancel
            </Button>
            <Button
              variant="danger"
              disabled={!emailMatches}
              onClick={() => setStage('final')}
            >
              Continue
            </Button>
          </Modal.Footer>
        </>
      )}

      {stage === 'final' && (
        <>
          <Modal.Body className="fs-10">
            <div className="alert alert-danger mb-3">
              <FontAwesomeIcon icon="exclamation-triangle" className="me-2" />
              This is the final confirmation. Pressing{' '}
              <strong>Erase my account</strong> runs the deletion immediately.
            </div>
            <p className="mb-0">
              You will be signed out and redirected to the login page. You will
              not be able to log back in.
            </p>
          </Modal.Body>
          <Modal.Footer>
            <Button
              variant="outline-secondary"
              onClick={handleClose}
              disabled={isProcessing}
            >
              Cancel
            </Button>
            <Button
              variant="danger"
              onClick={() => onConfirm()}
              disabled={isProcessing}
            >
              {isProcessing ? (
                <>
                  <Spinner animation="border" size="sm" className="me-2" />
                  Erasing…
                </>
              ) : (
                <>Erase my account</>
              )}
            </Button>
          </Modal.Footer>
        </>
      )}
    </Modal>
  );
};

export default EraseAccountModal;
