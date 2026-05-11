import { Suspense, lazy } from 'react';
import { useSearchParams } from 'react-router';
import { Card, Nav, Spinner, Tab } from 'react-bootstrap';

const PasswordTab = lazy(() => import('./PasswordTab'));
const MfaTab = lazy(() => import('./MfaTab'));
const LinkedProvidersTab = lazy(() => import('./LinkedProvidersTab'));
const SessionsTab = lazy(() => import('./SessionsTab'));
const TrustedDevicesTab = lazy(() => import('./TrustedDevicesTab'));
const BackupCodesTab = lazy(() => import('./BackupCodesTab'));

const TAB_KEYS = [
  'password',
  'mfa',
  'oauth',
  'sessions',
  'devices',
  'backup-codes',
] as const;
type TabKey = (typeof TAB_KEYS)[number];

const DEFAULT_TAB: TabKey = 'password';

// URL-tabs convention: persist the active tab to ?tab=X so the page
// is shareable + bookmarkable. Unknown values fall back to the
// password tab.
function readTab(param: string | null): TabKey {
  const candidate = (param ?? DEFAULT_TAB) as TabKey;
  return TAB_KEYS.includes(candidate) ? candidate : DEFAULT_TAB;
}

const SecurityPage = () => {
  const [searchParams, setSearchParams] = useSearchParams();
  const tab = readTab(searchParams.get('tab'));

  const onTabChange = (key: string | null) => {
    const next = readTab(key);
    const sp = new URLSearchParams(searchParams);
    if (next === DEFAULT_TAB) sp.delete('tab');
    else sp.set('tab', next);
    setSearchParams(sp, { replace: true });
  };

  return (
    <>
      <div className="mb-3">
        <h3 className="fw-normal mb-1">Account security</h3>
        <p className="fs-10 text-muted mb-0">
          Manage your password, second factors, and active sessions.
        </p>
      </div>

      <Card className="shadow-none border">
        <Tab.Container activeKey={tab} onSelect={onTabChange}>
          <Card.Header className="border-bottom border-200">
            <Nav variant="tabs" className="card-header-tabs fs-10">
              <Nav.Item>
                <Nav.Link eventKey="password">Password</Nav.Link>
              </Nav.Item>
              <Nav.Item>
                <Nav.Link eventKey="mfa">Two-factor</Nav.Link>
              </Nav.Item>
              <Nav.Item>
                <Nav.Link eventKey="oauth">Linked sign-ins</Nav.Link>
              </Nav.Item>
              <Nav.Item>
                <Nav.Link eventKey="sessions">Sessions</Nav.Link>
              </Nav.Item>
              <Nav.Item>
                <Nav.Link eventKey="devices">Trusted devices</Nav.Link>
              </Nav.Item>
              <Nav.Item>
                <Nav.Link eventKey="backup-codes">Backup codes</Nav.Link>
              </Nav.Item>
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
