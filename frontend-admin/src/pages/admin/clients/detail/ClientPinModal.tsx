import { Modal } from 'react-bootstrap';
import { useTranslation } from 'react-i18next';
import AdminPinSection from 'components/auth/pin/AdminPinSection';

interface ClientPinModalProps {
  target: { id: string; email: string } | null;
  onHide: () => void;
}

// PIN sign-in of a Tier-2 client user, opened from the members tab like the
// MFA reset: admins manage client users from the operator console.
const ClientPinModal = ({ target, onHide }: ClientPinModalProps) => {
  const { t } = useTranslation();
  return (
    <Modal show={!!target} onHide={onHide} size="lg" centered>
      <Modal.Header closeButton>
        <Modal.Title>
          {t('pinMethod.clientModalTitle', { email: target?.email ?? '' })}
        </Modal.Title>
      </Modal.Header>
      <Modal.Body>
        {target && <AdminPinSection userId={target.id} tier="client" />}
      </Modal.Body>
    </Modal>
  );
};

export default ClientPinModal;
