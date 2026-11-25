import { Card } from 'react-bootstrap';
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome';
import { faKey } from '@fortawesome/free-solid-svg-icons';
import { useTranslation } from 'react-i18next';
import AdminPinSection from 'components/auth/pin/AdminPinSection';
import type { User } from 'store/api/userApi';

// PIN sign-in card on /admin/user/profile/:userId (operator users). Kept
// apart from the Authentication Methods card because its permissions differ:
// that card reaches credentials under system.users.admin, this one follows
// the user routes (user.read to see, user.update + step-up to change).
const AdminPinCard = ({ user }: { user: User }) => {
  const { t } = useTranslation();
  return (
    <Card className="mb-3">
      <Card.Header className="bg-body-tertiary">
        <h5 className="mb-0">
          <FontAwesomeIcon icon={faKey} className="me-2" />
          {t('pinMethod.cardTitle')}
        </h5>
      </Card.Header>
      <Card.Body>
        <AdminPinSection userId={user.id} tier="operator" />
      </Card.Body>
    </Card>
  );
};

export default AdminPinCard;
