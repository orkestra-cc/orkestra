import { FontAwesomeIcon } from '@fortawesome/react-fontawesome';
import { useTranslation } from 'react-i18next';
import coverSrc from 'assets/img/generic/bg-1.jpg';
import defaultAvatar from 'assets/img/team/2.jpg';
import Flex from 'components/common/Flex';
import VerifiedBadge from 'components/common/VerifiedBadge';

import { Col, Row, Badge } from 'react-bootstrap';
import ProfileBanner from 'pages/admin/user-profile/AdminProfileBanner';
import { User } from 'store/api/userApi';

interface OperatorBannerProps {
  user: User;
}

const OperatorBanner: React.FC<OperatorBannerProps> = ({ user }) => {
  const { t } = useTranslation();

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
    if (!lastLogin) return t('profileShared.lastLoginNever');

    const loginDate = new Date(lastLogin);
    const now = new Date();
    const diffInHours = Math.floor(
      (now.getTime() - loginDate.getTime()) / (1000 * 60 * 60)
    );

    if (diffInHours < 1) return t('profileShared.lastLoginUnderHour');
    if (diffInHours < 24)
      return t(
        diffInHours === 1
          ? 'profileShared.lastLoginHoursAgoOne'
          : 'profileShared.lastLoginHoursAgoOther',
        { count: diffInHours }
      );
    if (diffInHours < 48) return t('profileShared.lastLoginOneDayAgo');
    return t('profileShared.lastLoginDaysAgoOther', {
      count: Math.floor(diffInHours / 24)
    });
  };

  const roleLabel = t(`adminUsers.roles.${user.role}`, {
    defaultValue: user.role
  });

  return (
    <ProfileBanner>
      <ProfileBanner.Header
        avatar={user.avatar || defaultAvatar}
        coverSrc={coverSrc}
      />
      <ProfileBanner.Body>
        <Row>
          <Col lg={12}>
            <Flex alignItems="center" className="mb-2">
              <h4 className="mb-0 me-2">
                {user.fullName} {user.emailVerified && <VerifiedBadge />}
              </h4>
              <Badge bg={user.isActive ? 'success' : 'danger'} className="ms-2">
                {user.isActive
                  ? t('profileShared.statusActive')
                  : t('profileShared.statusInactive')}
              </Badge>
            </Flex>
            <h5 className="fs-9 fw-normal">{user.email}</h5>
            <Flex className="mb-3 mt-2">
              <small className="text-700 me-3">
                <FontAwesomeIcon icon="calendar-alt" className="me-1" />
                {t('profileShared.registeredLabel', {
                  date: formatDate(user.createdAt)
                })}
              </small>
              <small className="text-700 me-3">
                <FontAwesomeIcon icon="clock" className="me-1" />
                {t('profileShared.lastLoginLabel', {
                  when: formatLastLogin(user.lastLogin)
                })}
              </small>
              <small className="text-700">
                <FontAwesomeIcon icon="shield-alt" className="me-1" />
                {t('profileShared.roleLabel', { role: roleLabel })}
              </small>
            </Flex>
          </Col>
        </Row>
      </ProfileBanner.Body>
    </ProfileBanner>
  );
};

export default OperatorBanner;
