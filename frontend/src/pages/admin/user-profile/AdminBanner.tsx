import { FontAwesomeIcon } from '@fortawesome/react-fontawesome';
import coverSrc from 'assets/img/generic/bg-1.jpg';
import defaultAvatar from 'assets/img/team/2.jpg';
import Flex from 'components/common/Flex';
import VerifiedBadge from 'components/common/VerifiedBadge';

import { Col, Row, Badge } from 'react-bootstrap';
import ProfileBanner from './AdminProfileBanner';
import { User } from 'store/api/userApi';

interface AdminBannerProps {
  user: User;
}

const AdminBanner: React.FC<AdminBannerProps> = ({ user }) => {
  // Helper function to format date
  const formatDate = (dateString: string) => {
    return new Date(dateString).toLocaleDateString('en-GB', {
      year: 'numeric',
      month: 'long',
      day: 'numeric'
    });
  };

  // Helper function to format last login
  const formatLastLogin = (lastLogin?: string) => {
    if (!lastLogin) return 'Never';

    const loginDate = new Date(lastLogin);
    const now = new Date();
    const diffInHours = Math.floor(
      (now.getTime() - loginDate.getTime()) / (1000 * 60 * 60)
    );

    if (diffInHours < 1) return 'Less than an hour ago';
    if (diffInHours < 24) return `${diffInHours} hours ago`;
    if (diffInHours < 48) return '1 day ago';
    return `${Math.floor(diffInHours / 24)} days ago`;
  };

  // Role labels
  const roleLabels: Record<string, string> = {
    developer: 'Developer',
    ceo: 'CEO',
    administrator: 'Administrator',
    manager: 'Manager',
    operator: 'Operator',
    guest: 'Guest'
  };

  return (
    <ProfileBanner>
      <ProfileBanner.Header
        avatar={user.avatar || defaultAvatar}
        coverSrc={coverSrc}
      />
      <ProfileBanner.Body>
        <Row>
          <Col lg={8}>
            <Flex alignItems="center" className="mb-2">
              <h4 className="mb-0 me-2">
                {user.fullName} {user.emailVerified && <VerifiedBadge />}
              </h4>
              <Badge bg={user.isActive ? 'success' : 'danger'} className="ms-2">
                {user.isActive ? 'Active' : 'Inactive'}
              </Badge>
            </Flex>
            <h5 className="fs-9 fw-normal">{user.email}</h5>
            <Flex className="mb-3 mt-2">
              <small className="text-700 me-3">
                <FontAwesomeIcon icon="calendar-alt" className="me-1" />
                Registered: {formatDate(user.createdAt)}
              </small>
              <small className="text-700 me-3">
                <FontAwesomeIcon icon="clock" className="me-1" />
                Last Login: {formatLastLogin(user.lastLogin)}
              </small>
              <small className="text-700">
                <FontAwesomeIcon icon="shield-alt" className="me-1" />
                Role: {roleLabels[user.role] || user.role}
              </small>
            </Flex>
            {/* <Button variant="primary" size="sm" className="px-3 me-2">
              <FontAwesomeIcon icon="edit" className="me-1" />
              Modifica Profilo
            </Button>
            <Button variant="warning" size="sm" className="px-3 me-2">
              <FontAwesomeIcon icon="lock" className="me-1" />
              Reimposta Password
            </Button>
            <Button variant="danger" size="sm" className="px-3">
              <FontAwesomeIcon icon="user-slash" className="me-1" />
              Sospendi Account
            </Button> */}
            {/* <div className="border-dashed border-bottom my-4 d-lg-none" /> */}
          </Col>
          {/* <Col className="ps-2 ps-lg-3">
            <Flex alignItems="center" className="mb-0">
              <FontAwesomeIcon
                icon="shield-alt"
                className="me-2 text-primary"
                style={{ height: '24px', width: '24px' }}
              />
            </Flex>
            <Flex alignItems="center" className="mb-3">
              <FontAwesomeIcon
                icon="chart-line"
                className="me-2 text-success"
                style={{ height: '24px', width: '24px' }}
              />
              <div className="flex-1">
                <h6 className="mb-0">Attività di Login</h6>
                <small className="text-600">45 accessi questo mese</small>
              </div>
            </Flex>
            <Flex alignItems="center" className="mb-3">
              <FontAwesomeIcon
                icon="tasks"
                className="me-2 text-info"
                style={{ height: '24px', width: '24px' }}
              />
              <div className="flex-1">
                <h6 className="mb-0">Attività Attive</h6>
                <small className="text-600">12 compiti assegnati</small>
              </div>
            </Flex>
          </Col> */}
        </Row>
      </ProfileBanner.Body>
    </ProfileBanner>
  );
};

export default AdminBanner;
