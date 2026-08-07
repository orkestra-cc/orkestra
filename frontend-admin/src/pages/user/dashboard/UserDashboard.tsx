import { Col, Row } from 'react-bootstrap';
import {
  faDesktop,
  faKey,
  faLaptop,
  faLayerGroup,
  faShieldHalved
} from '@fortawesome/free-solid-svg-icons';
import { Link } from 'react-router';
import { useTranslation } from 'react-i18next';
import PageHeader from 'components/common/PageHeader';
import StatCard from 'components/common/StatCard';
import SectionCard from 'components/common/SectionCard';
import SubtleBadge from 'components/common/SubtleBadge';
import OperatorMembershipsCard from 'components/common/OperatorMembershipsCard';
import { useAuth } from 'hooks/auth/useAuthRTK';
import { useGetMySessionsQuery } from 'store/api/authApi';
import { useListTrustedDevicesQuery } from 'store/api/deviceTrustApi';
import { useGetMfaStatusQuery } from 'store/api/mfaApi';
import { useListMyOrgsQuery } from 'store/api/tenantApi';
import { formatDateTime } from 'helpers/dateFormat';

// The signed-in operator's landing page (DEFAULT_POST_LOGIN): a day-start
// digest built only from data the core APIs already serve — active sessions,
// trusted devices, MFA posture, memberships — with deep links into
// /user/security's tabs. Every number here is the same one the security
// center itself renders; nothing is mocked or estimated.

/** One labelled row of the security digest. */
const DigestRow: React.FC<{
  label: string;
  children: React.ReactNode;
  last?: boolean;
}> = ({ label, children, last }) => (
  <div
    className={`d-flex flex-wrap align-items-center justify-content-between gap-2 py-2 ${
      last ? '' : 'border-bottom border-200'
    }`}
  >
    <span className="text-700">{label}</span>
    <span className="d-flex align-items-center gap-2 text-900">{children}</span>
  </div>
);

const UserDashboard = () => {
  const { t } = useTranslation();
  const { user } = useAuth();
  const sessions = useGetMySessionsQuery();
  const devices = useListTrustedDevicesQuery();
  const mfa = useGetMfaStatusQuery();
  const orgs = useListMyOrgsQuery(undefined);

  const firstName =
    (user?.fullName || user?.username || '').trim().split(/\s+/)[0] || '';
  const mfaEnrolled = mfa.data?.status === 'enrolled';
  // Attention only when the role obligates enrollment and it hasn't happened
  // — an operator whose role doesn't require MFA gets a neutral "Off", not
  // an alarm.
  const mfaAttention = Boolean(
    mfa.data && !mfaEnrolled && mfa.data.requiresMfa
  );
  const backupCodes = mfa.data?.backupCodesRemaining ?? 0;

  return (
    <>
      <PageHeader
        title={
          firstName
            ? t('userDashboard.greeting', { name: firstName })
            : t('userDashboard.pageTitle')
        }
        description={t('userDashboard.pageDescription')}
        className="mb-3"
      />

      <Row className="g-3 mb-3">
        <Col md={6} lg={3}>
          <StatCard
            title={t('userDashboard.stats.sessions.title')}
            value={sessions.data?.activeCount ?? 0}
            icon={faDesktop}
            color="primary"
            subtitle={sessions.data?.currentDevice}
            loading={sessions.isLoading}
          />
        </Col>
        <Col md={6} lg={3}>
          <StatCard
            title={t('userDashboard.stats.devices.title')}
            value={devices.data?.devices?.length ?? 0}
            icon={faLaptop}
            color="info"
            loading={devices.isLoading}
          />
        </Col>
        <Col md={6} lg={3}>
          <StatCard
            title={t('userDashboard.stats.mfa.title')}
            value={
              mfaEnrolled
                ? t('userDashboard.stats.mfa.active')
                : t('userDashboard.stats.mfa.inactive')
            }
            icon={faKey}
            color="success"
            accent={mfaAttention ? 'warning' : undefined}
            badge={
              mfaAttention
                ? {
                    text: t('userDashboard.stats.mfa.setupBadge'),
                    bg: 'warning'
                  }
                : undefined
            }
            loading={mfa.isLoading}
          />
        </Col>
        <Col md={6} lg={3}>
          <StatCard
            title={t('userDashboard.stats.orgs.title')}
            value={orgs.data?.memberships?.length ?? 0}
            icon={faLayerGroup}
            color="primary"
            loading={orgs.isLoading}
          />
        </Col>
      </Row>

      <Row className="g-3">
        <Col lg={7}>
          <SectionCard
            icon={faShieldHalved}
            iconColor="primary"
            title={t('userDashboard.security.title')}
            headerEnd={
              <Link to="/user/security" className="fs-10 fw-semibold">
                {t('userDashboard.security.open')}
              </Link>
            }
          >
            <div className="fs-10">
              <DigestRow label={t('userDashboard.security.email')}>
                {user?.email}
                <SubtleBadge
                  bg={user?.emailVerified ? 'success' : 'warning'}
                  pill
                  className="fs-11"
                >
                  {user?.emailVerified
                    ? t('userDashboard.security.emailVerified')
                    : t('userDashboard.security.emailUnverified')}
                </SubtleBadge>
              </DigestRow>
              <DigestRow label={t('userDashboard.security.mfaRow')}>
                <SubtleBadge
                  bg={
                    mfaEnrolled
                      ? 'success'
                      : mfaAttention
                        ? 'warning'
                        : 'secondary'
                  }
                  pill
                  className="fs-11"
                >
                  {mfaEnrolled
                    ? t('userDashboard.stats.mfa.active')
                    : t('userDashboard.stats.mfa.inactive')}
                </SubtleBadge>
                {!mfaEnrolled && (
                  <Link to="/user/security?tab=mfa" className="fw-semibold">
                    {t('userDashboard.security.mfaConfigure')}
                  </Link>
                )}
              </DigestRow>
              {mfaEnrolled && (
                <DigestRow label={t('userDashboard.security.backupCodes')}>
                  {t('userDashboard.security.backupCodesLeft', {
                    count: backupCodes
                  })}
                  {backupCodes === 0 && (
                    <Link
                      to="/user/security?tab=backup-codes"
                      className="fw-semibold"
                    >
                      {t('userDashboard.security.backupCodesRegenerate')}
                    </Link>
                  )}
                </DigestRow>
              )}
              <DigestRow label={t('userDashboard.security.lastLogin')} last>
                {formatDateTime(user?.lastLogin)}
              </DigestRow>
            </div>
          </SectionCard>
        </Col>
        <Col lg={5}>
          <OperatorMembershipsCard />
        </Col>
      </Row>
    </>
  );
};

export default UserDashboard;
