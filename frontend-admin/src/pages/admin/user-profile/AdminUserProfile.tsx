import { useParams } from 'react-router';
import { useGetUserByIdQuery } from 'store/api/userApi';
import AdminBanner from './AdminBanner';
import AdminProfileIntro from './AdminProfileIntro';
import { Col, Row, Alert, Spinner } from 'react-bootstrap';
import { useTranslation } from 'react-i18next';

import AdminUserActions from './AdminUserActions';
import AdminUserMetrics from './AdminUserMetrics';
import AdminAuthMethodsCard from './AdminAuthMethodsCard';
import { useSlotContributions } from 'modules/useSlotContributions';
import ContributedProfileSections from 'pages/user/ContributedProfileSections';
import {
  USER_ADMIN_PROFILE_SECTIONS,
  type UserProfileSectionProps
} from 'pages/user/profileSlots';

const AdminUserProfile: React.FC = () => {
  const { t } = useTranslation();
  const { userId } = useParams<{ userId: string }>();
  const sections = useSlotContributions(USER_ADMIN_PROFILE_SECTIONS);

  const {
    data: user,
    isLoading,
    error
  } = useGetUserByIdQuery(userId!, {
    skip: !userId
  });

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

  if (!user) {
    return <Alert variant="warning">{t('profileShared.userNotFound')}</Alert>;
  }

  return (
    <>
      <AdminBanner user={user} />
      <Row className="g-3 mb-3">
        <Col lg={8}>
          <AdminProfileIntro user={user} />
          <AdminAuthMethodsCard user={user} />
          <ContributedProfileSections<UserProfileSectionProps>
            contributions={sections}
            sectionProps={{ userId: user.id, user, viewer: 'admin' }}
          />
        </Col>
        <Col lg={4}>
          <div className="sticky-sidebar">
            <AdminUserActions user={user} />
            <AdminUserMetrics userId={userId!} />
          </div>
        </Col>
      </Row>
    </>
  );
};

export default AdminUserProfile;
