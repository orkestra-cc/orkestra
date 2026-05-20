import { Modal, Button, Spinner } from 'react-bootstrap';
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome';
import {
  faExclamationTriangle,
  faTimes
} from '@fortawesome/free-solid-svg-icons';
import { Trans, useTranslation } from 'react-i18next';

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
  confirmText,
  confirmVariant = 'danger'
}) => {
  const { t } = useTranslation();
  return (
    <Modal show={show} onHide={onHide} centered>
      <Modal.Header>
        <Modal.Title
          className={confirmVariant === 'danger' ? 'text-danger' : ''}
        >
          {confirmVariant === 'danger' && (
            <FontAwesomeIcon icon={faExclamationTriangle} className="me-2" />
          )}
          {title || t('documents.templates.deleteModal.title')}
        </Modal.Title>
        <Button
          variant="link"
          className="p-0 text-decoration-none"
          onClick={onHide}
        >
          <FontAwesomeIcon icon={faTimes} />
        </Button>
      </Modal.Header>

      <Modal.Body>
        {body || (
          <p>
            <Trans
              i18nKey="documents.templates.deleteModal.body"
              values={{ name: templateName }}
              components={{ strong: <strong /> }}
            />
            <br />
            <span className="text-muted">
              {t('documents.templates.deleteModal.warning')}
            </span>
          </p>
        )}
      </Modal.Body>

      <Modal.Footer>
        <Button variant="secondary" onClick={onHide} disabled={isLoading}>
          {t('documents.templates.deleteModal.cancel')}
        </Button>
        <Button
          variant={confirmVariant}
          onClick={onConfirm}
          disabled={isLoading}
        >
          {isLoading ? (
            <Spinner animation="border" size="sm" className="me-1" />
          ) : null}
          {confirmText || t('documents.templates.deleteModal.confirm')}
        </Button>
      </Modal.Footer>
    </Modal>
  );
};

export default DeleteConfirmModal;
