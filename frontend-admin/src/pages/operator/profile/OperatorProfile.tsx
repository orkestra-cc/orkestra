import { useGetCurrentUserQuery } from 'store/api/authApi';
import OperatorBanner from './OperatorBanner';
import OperatorProfileIntro from './OperatorProfileIntro';
import OperatorMembershipsCard from 'components/common/OperatorMembershipsCard';
import { Col, Row, Alert, Spinner } from 'react-bootstrap';
import { useTranslation } from 'react-i18next';
import type { User } from 'store/api/userApi';
import { useSlotContributions } from 'modules/useSlotContributions';
import ContributedProfileSections from 'pages/user/ContributedProfileSections';
import {
  USER_PROFILE_SECTIONS,
  type UserProfileSectionProps
} from 'pages/user/profileSlots';

const OperatorProfile: React.FC = () => {
  const { t } = useTranslation();
  const { data: backendUser, isLoading, error } = useGetCurrentUserQuery();
  const sections = useSlotContributions(USER_PROFILE_SECTIONS);

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
        <Col lg={12}>
          <OperatorMembershipsCard />
        </Col>
        <Col lg={12}>
          <ContributedProfileSections<UserProfileSectionProps>
            contributions={sections}
            sectionProps={{ userId: user.id, user, viewer: 'self' }}
          />
        </Col>
      </Row>
    </>
  );
};

export default OperatorProfile;
