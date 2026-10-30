import { Button, Modal } from 'react-bootstrap';
import { useTranslation } from 'react-i18next';
import { formatDateTime } from 'helpers/dateFormat';
import type { PinEnrollmentCode } from 'store/api/pinApi';

interface EnrollmentCodeModalProps {
  code: PinEnrollmentCode | null;
  onHide: () => void;
}

// Shows a one-time device enrollment code. It is never fetched again: the
// server keeps only its hash, so closing the modal loses it for good.
const EnrollmentCodeModal = ({ code, onHide }: EnrollmentCodeModalProps) => {
  const { t } = useTranslation();
  return (
    <Modal show={!!code} onHide={onHide} centered>
      <Modal.Header closeButton>
        <Modal.Title>{t('pinMethod.codeModal.title')}</Modal.Title>
      </Modal.Header>
      <Modal.Body>
        <p className="fs-10 text-700">{t('pinMethod.codeModal.intro')}</p>
        <div className="text-center my-4">
          <span
            className="font-monospace fs-4 fw-bold text-900 user-select-all"
            data-testid="pin-enrollment-code"
          >
            {code?.code}
          </span>
        </div>
        {code && (
          <p className="fs-10 text-700 mb-0">
            {t('pinMethod.codeModal.expires', {
              when: formatDateTime(code.expiresAt)
            })}
          </p>
        )}
      </Modal.Body>
      <Modal.Footer>
        <Button variant="orkestra-primary" onClick={onHide}>
          {t('pinMethod.codeModal.done')}
        </Button>
      </Modal.Footer>
    </Modal>
  );
};

export default EnrollmentCodeModal;
