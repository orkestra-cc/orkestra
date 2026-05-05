import { Modal, Button, Spinner } from 'react-bootstrap';
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome';
import { faExclamationTriangle, faTimes } from '@fortawesome/free-solid-svg-icons';

interface DeleteConfirmModalProps {
  show: boolean;
  onHide: () => void;
  onConfirm: () => void;
  isLoading?: boolean;
  templateName: string;
  title?: string;
  body?: React.ReactNode;
  confirmText?: string;
  confirmVariant?: string;
}

const DeleteConfirmModal: React.FC<DeleteConfirmModalProps> = ({
  show,
  onHide,
  onConfirm,
  isLoading = false,
  templateName,
  title,
  body,
  confirmText = 'Elimina',
  confirmVariant = 'danger',
}) => {
  return (
    <Modal show={show} onHide={onHide} centered>
      <Modal.Header>
        <Modal.Title className={confirmVariant === 'danger' ? 'text-danger' : ''}>
          {confirmVariant === 'danger' && (
            <FontAwesomeIcon icon={faExclamationTriangle} className="me-2" />
          )}
          {title || 'Conferma eliminazione'}
        </Modal.Title>
        <Button variant="link" className="p-0 text-decoration-none" onClick={onHide}>
          <FontAwesomeIcon icon={faTimes} />
        </Button>
      </Modal.Header>

      <Modal.Body>
        {body || (
          <p>
            Sei sicuro di voler eliminare il template{' '}
            <strong>{templateName}</strong>?
            <br />
            <span className="text-muted">Questa azione non può essere annullata.</span>
          </p>
        )}
      </Modal.Body>

      <Modal.Footer>
        <Button variant="secondary" onClick={onHide} disabled={isLoading}>
          Annulla
        </Button>
        <Button
          variant={confirmVariant}
          onClick={onConfirm}
          disabled={isLoading}
        >
          {isLoading ? (
            <Spinner animation="border" size="sm" className="me-1" />
          ) : null}
          {confirmText}
        </Button>
      </Modal.Footer>
    </Modal>
  );
};

export default DeleteConfirmModal;
