import { Alert, Button, Modal } from 'react-bootstrap';
import { useTranslation } from 'react-i18next';

interface Props {
  show: boolean;
  title: string;
  body: string;
  error?: string | null;
  busy?: boolean;
  onCancel: () => void;
  onConfirm: () => void;
  onExited?: () => void;
}

// ConfirmDeleteModal names what is about to be deleted. It stays mounted and
// toggles `show`, so react-bootstrap's restoreFocus hands focus back to the
// row action that opened it when the operator cancels.
const ConfirmDeleteModal = ({
  show,
  title,
  body,
  error,
  busy,
  onCancel,
  onConfirm,
  onExited
}: Props) => {
  const { t } = useTranslation();
  return (
    <Modal show={show} onHide={onCancel} onExited={onExited} centered>
      <Modal.Header closeButton>
        <Modal.Title as="h5">{title}</Modal.Title>
      </Modal.Header>
      <Modal.Body>
        {error && (
          <Alert variant="danger" className="fs-10 py-2">
            {error}
          </Alert>
        )}
        <p className="mb-0">{body}</p>
      </Modal.Body>
      <Modal.Footer>
        <Button variant="orkestra-default" onClick={onCancel}>
          {t('adminLlm.actions.cancel')}
        </Button>
        <Button variant="orkestra-danger" disabled={busy} onClick={onConfirm}>
          {t('adminLlm.actions.delete')}
        </Button>
      </Modal.Footer>
    </Modal>
  );
};

export default ConfirmDeleteModal;
