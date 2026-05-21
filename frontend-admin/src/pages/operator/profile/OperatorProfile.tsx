import { useGetCurrentUserQuery } from 'store/api/authApi';
import OperatorBanner from './OperatorBanner';
import OperatorProfileIntro from './OperatorProfileIntro';
import { Col, Row, Alert, Spinner } from 'react-bootstrap';
import { useTranslation } from 'react-i18next';
import type { User } from 'store/api/userApi';

const OperatorProfile: React.FC = () => {
  const { t } = useTranslation();
  const { data: backendUser, isLoading, error } = useGetCurrentUserQuery();

  if (isLoading) {
    return (
      <div
        className="d-flex justify-content-center align-items-center"
        style={{ minHeight: '400px' }}
      >
        <Spinner animation="border" role="status">
          <span className="visually-hidden">
            {t('profileShared.loadingAria')}
          </span>
        </Spinner>
      </div>
    );
  }

  if (error) {
    return <Alert variant="danger">{t('profileShared.errorLoad')}</Alert>;
  }

  if (!backendUser) {
    return <Alert variant="warning">{t('profileShared.userNotFound')}</Alert>;
  }

  const user: User = {
    id: backendUser.id,
    email: backendUser.email,
    username: backendUser.username,
    fullName: backendUser.fullName,
    avatar: backendUser.avatar,
    role: backendUser.role,
    providers: backendUser.oauthProviders ?? [],
    isActive: backendUser.isActive,
    emailVerified: backendUser.emailVerified,
    lastLogin: backendUser.lastLogin,
    createdAt: backendUser.createdAt,
    updatedAt: backendUser.updatedAt
  };

  return (
    <>
      <OperatorBanner user={user} />
      <Row className="g-3 mb-3">
        <Col lg={12}>
          <OperatorProfileIntro user={user} />
        </Col>
      </Row>
    </>
  );
};

export default OperatorProfile;
