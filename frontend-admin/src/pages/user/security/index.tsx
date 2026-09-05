import { Suspense, lazy } from 'react';
import { useSearchParams } from 'react-router';
import { Card, Col, Nav, Row, Spinner, Tab } from 'react-bootstrap';
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome';
import {
  faDesktop,
  faKey,
  faLaptop,
  faLifeRing,
  faLink,
  faShieldHalved
} from '@fortawesome/free-solid-svg-icons';
import { useTranslation } from 'react-i18next';
import StatCard from 'components/common/StatCard';
import {
  useGetMySessionsQuery,
  useGetSelfAuthMethodsQuery
} from 'store/api/authApi';
import { useListTrustedDevicesQuery } from 'store/api/deviceTrustApi';

const PasswordTab = lazy(() => import('./PasswordTab'));
const MfaTab = lazy(() => import('./MfaTab'));
const LinkedProvidersTab = lazy(() => import('./LinkedProvidersTab'));
const SessionsTab = lazy(() => import('./SessionsTab'));
const TrustedDevicesTab = lazy(() => import('./TrustedDevicesTab'));
const BackupCodesTab = lazy(() => import('./BackupCodesTab'));

const TABS = [
  { key: 'password', labelKey: 'userSecurity.tabs.password', icon: faKey },
  { key: 'mfa', labelKey: 'userSecurity.tabs.mfa', icon: faShieldHalved },
  { key: 'oauth', labelKey: 'userSecurity.tabs.oauth', icon: faLink },
  { key: 'sessions', labelKey: 'userSecurity.tabs.sessions', icon: faDesktop },
  { key: 'devices', labelKey: 'userSecurity.tabs.devices', icon: faLaptop },
  {
    key: 'backup-codes',
    labelKey: 'userSecurity.tabs.backupCodes',
    icon: faLifeRing
  }
] as const;

const TAB_KEYS = TABS.map(tab => tab.key);
type TabKey = (typeof TABS)[number]['key'];

const DEFAULT_TAB: TabKey = 'password';

// URL-tabs convention: persist the active tab to ?tab=X so the page
// is shareable + bookmarkable. Unknown values fall back to the
// password tab.
function readTab(param: string | null): TabKey {
  const candidate = (param ?? DEFAULT_TAB) as TabKey;
  return (TAB_KEYS as readonly string[]).includes(candidate)
    ? candidate
    : DEFAULT_TAB;
}

const SecurityPage = () => {
  const { t } = useTranslation();
  const [searchParams, setSearchParams] = useSearchParams();
  const tab = readTab(searchParams.get('tab'));

  // The KPI row reads the same RTK Query caches the tabs read, so it costs no
  // extra round trip — RTK Query dedupes the in-flight requests (the pattern
  // /admin/compliance's summary row uses).
  const authMethods = useGetSelfAuthMethodsQuery();
  const sessions = useGetMySessionsQuery();
  const trusted = useListTrustedDevicesQuery();

  const totpEnrolled = !!authMethods.data?.mfaFactors.find(
    f => f.type === 'totp'
  );
  const passkeyCount =
    authMethods.data?.mfaFactors.find(f => f.type === 'webauthn')?.credentials
      ?.length ?? 0;
  const hasMfa = totpEnrolled || passkeyCount > 0;
  const mfaRequired = !!authMethods.data?.mfaRequired;

  const mfaMethods: string[] = [];
  if (totpEnrolled) mfaMethods.push(t('userSecurity.stats.mfaMethodTotp'));
  if (passkeyCount > 0) {
    mfaMethods.push(
      t(
        passkeyCount === 1
          ? 'userSecurity.stats.mfaMethodPasskeyOne'
          : 'userSecurity.stats.mfaMethodPasskeyOther',
        { count: passkeyCount }
      )
    );
  }

  const sessionCount = sessions.data?.activeCount ?? 0;
  const providerCount = authMethods.data?.oauthProviders?.length ?? 0;
  const trustedCount = trusted.data?.devices?.length ?? 0;

  const onTabChange = (key: string | null) => {
    const next = readTab(key);
    const sp = new URLSearchParams(searchParams);
    if (next === DEFAULT_TAB) sp.delete('tab');
    else sp.set('tab', next);
    setSearchParams(sp, { replace: true });
  };

  return (
    <>
      {/* h3 in a card body = the shared admin page-header genre
          (/admin/users, /admin/modules, /admin/compliance), so this surface
          reads as part of the same page family instead of floating bare on
          the canvas. */}
      <Card className="mb-3">
        <Card.Body className="py-3 px-4">
          <h3 className="mb-1">{t('userSecurity.pageTitle')}</h3>
          <p className="text-muted mb-0 fs-10">
            {t('userSecurity.pageSubtitle')}
          </p>
        </Card.Body>
      </Card>

      <Row className="g-3 mb-3">
        <Col md={6} lg={3}>
          <StatCard
            title={t('userSecurity.stats.mfaTitle')}
            value={t(
              hasMfa ? 'userSecurity.stats.mfaOn' : 'userSecurity.stats.mfaOff'
            )}
            icon={faShieldHalved}
            color={hasMfa ? 'success' : 'warning'}
            // Accent only when the state earns it — an account WITH a second
            // factor is the resting state, not an event.
            accent={hasMfa ? undefined : 'warning'}
            subtitle={
              mfaMethods.length > 0
                ? mfaMethods.join(' · ')
                : t('userSecurity.stats.mfaSubtitleNone')
            }
            badge={
              !hasMfa && mfaRequired
                ? { text: t('userSecurity.stats.mfaRequiredBadge') }
                : undefined
            }
            loading={authMethods.isLoading}
          />
        </Col>
        <Col md={6} lg={3}>
          <StatCard
            title={t('userSecurity.stats.sessionsTitle')}
            value={sessionCount}
            icon={faDesktop}
            color="info"
            subtitle={t('userSecurity.stats.sessionsSubtitle')}
            loading={sessions.isLoading}
          />
        </Col>
        <Col md={6} lg={3}>
          <StatCard
            title={t('userSecurity.stats.providersTitle')}
            value={providerCount}
            icon={faLink}
            color="primary"
            subtitle={t('userSecurity.stats.providersSubtitle')}
            loading={authMethods.isLoading}
          />
        </Col>
        <Col md={6} lg={3}>
          <StatCard
            title={t('userSecurity.stats.devicesTitle')}
            value={trustedCount}
            icon={faLaptop}
            color="secondary"
            subtitle={t('userSecurity.stats.devicesSubtitle')}
            loading={trusted.isLoading}
          />
        </Col>
      </Row>

      {/* card-header-tabs is the one tab primitive console-wide
          (/admin/compliance, clients/detail, internal-tenants/detail). The tab
          strip names the section, so the panes below carry no card of their
          own — a second card inside this body was double chrome repeating the
          label already in the strip. */}
      <Card className="shadow-none border">
        <Tab.Container activeKey={tab} onSelect={onTabChange}>
          <Card.Header className="border-bottom border-200">
            <Nav variant="tabs" className="card-header-tabs fs-10">
              {TABS.map(item => (
                <Nav.Item key={item.key}>
                  <Nav.Link eventKey={item.key} className="text-nowrap">
                    <FontAwesomeIcon icon={item.icon} className="me-2" />
                    {t(item.labelKey)}
                  </Nav.Link>
                </Nav.Item>
              ))}
            </Nav>
          </Card.Header>
          <Card.Body>
            <Suspense
              fallback={
                <div className="text-center py-4">
                  <Spinner animation="border" size="sm" />
                </div>
              }
            >
              <Tab.Content>
                <Tab.Pane eventKey="password">
                  <PasswordTab />
                </Tab.Pane>
                <Tab.Pane eventKey="mfa">
                  <MfaTab />
                </Tab.Pane>
                <Tab.Pane eventKey="oauth">
                  <LinkedProvidersTab />
                </Tab.Pane>
                <Tab.Pane eventKey="sessions">
                  <SessionsTab />
                </Tab.Pane>
                <Tab.Pane eventKey="devices">
                  <TrustedDevicesTab />
                </Tab.Pane>
                <Tab.Pane eventKey="backup-codes">
                  <BackupCodesTab />
                </Tab.Pane>
              </Tab.Content>
            </Suspense>
          </Card.Body>
        </Tab.Container>
      </Card>
    </>
  );
};

export default SecurityPage;
