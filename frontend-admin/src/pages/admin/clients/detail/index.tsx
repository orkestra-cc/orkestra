import { Suspense, lazy, useMemo } from 'react';
import { Link, Navigate, useParams, useSearchParams } from 'react-router';
import { Alert, Breadcrumb, Card, Nav, Spinner, Tab } from 'react-bootstrap';
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome';
import SubtleBadge from 'components/common/SubtleBadge';
import type { BadgeColor } from 'components/common/SubtleBadge';
import { useGetOrgAdminQuery } from 'store/api/tenantApi';

const OverviewTab = lazy(() => import('./OverviewTab'));
const MembersTab = lazy(() => import('./MembersTab'));
const DivisionsTab = lazy(() => import('./DivisionsTab'));
const SubscriptionsTab = lazy(() => import('./SubscriptionsTab'));
const PaymentsTab = lazy(() => import('./PaymentsTab'));
const ActivityTab = lazy(() => import('./ActivityTab'));

const TAB_KEYS = [
  'overview',
  'members',
  'divisions',
  'subscriptions',
  'payments',
  'activity',
] as const;
type TabKey = (typeof TAB_KEYS)[number];

const DEFAULT_TAB: TabKey = 'overview';

// URL-tabs convention: active tab persists to ?tab=X so the page is
// shareable and bookmarkable. Unknown ?tab values fall back to Overview.
function readTab(param: string | null): TabKey {
  const candidate = (param ?? DEFAULT_TAB) as TabKey;
  return TAB_KEYS.includes(candidate) ? candidate : DEFAULT_TAB;
}

const planColors: Record<string, BadgeColor> = {
  free: 'secondary',
  pro: 'primary',
  enterprise: 'success',
};

const ClientDetailPage: React.FC = () => {
  const { clientId } = useParams<{ clientId: string }>();
  const [searchParams, setSearchParams] = useSearchParams();
  const tab = readTab(searchParams.get('tab'));

  const { data: org, isLoading, error } = useGetOrgAdminQuery(clientId ?? '', {
    skip: !clientId,
  });

  const statusBadge = useMemo(() => {
    if (!org) return null;
    if (org.status === 'purged')
      return { bg: 'dark' as BadgeColor, label: 'purged' };
    if (org.status === 'archived' || org.archivedAt)
      return { bg: 'danger' as BadgeColor, label: 'archived' };
    if (org.status === 'suspended')
      return { bg: 'warning' as BadgeColor, label: 'suspended' };
    if (org.status === 'provisioning')
      return { bg: 'info' as BadgeColor, label: 'provisioning' };
    return { bg: 'success' as BadgeColor, label: 'active' };
  }, [org]);

  if (!clientId) {
    return <Navigate to="/admin/clients" replace />;
  }

  if (isLoading) {
    return (
      <div className="text-center py-5">
        <Spinner animation="border" size="sm" />
      </div>
    );
  }

  if (error || !org) {
    return (
      <Alert variant="danger">
        Client not found or you lack permission to view it.{' '}
        <Link to="/admin/clients">Back to clients</Link>
      </Alert>
    );
  }

  // Defence-in-depth: if someone deep-links an internal tenant into the
  // client detail route, redirect them to the right page rather than
  // rendering client-only tabs (Divisions, Subscriptions, Payments) that
  // would be empty or misleading.
  if (org.kind === 'internal') {
    return <Navigate to={`/admin/internal/tenants/${clientId}`} replace />;
  }

  const onTabChange = (key: string | null) => {
    const next = readTab(key);
    const sp = new URLSearchParams(searchParams);
    if (next === DEFAULT_TAB) sp.delete('tab');
    else sp.set('tab', next);
    setSearchParams(sp, { replace: true });
  };

  return (
    <>
      <Breadcrumb className="mb-3 fs-10">
        <Breadcrumb.Item linkAs={Link} linkProps={{ to: '/admin/clients' }}>
          Clients
        </Breadcrumb.Item>
        <Breadcrumb.Item active>{org.name}</Breadcrumb.Item>
      </Breadcrumb>

      <Card className="mb-3 shadow-none border">
        <Card.Body className="d-flex justify-content-between align-items-start flex-wrap gap-3">
          <div>
            <h3 className="fw-normal mb-1">
              <FontAwesomeIcon icon="users" className="text-primary me-2" />
              {org.name}
            </h3>
            <div className="d-flex align-items-center gap-2 flex-wrap fs-10 text-muted">
              <code className="fs-11">{org.slug}</code>
              {statusBadge && (
                <SubtleBadge bg={statusBadge.bg} pill>
                  {statusBadge.label}
                </SubtleBadge>
              )}
              <SubtleBadge bg={planColors[org.plan] || 'secondary'} pill>
                {org.plan}
              </SubtleBadge>
              <SubtleBadge bg="info" pill>
                external
              </SubtleBadge>
            </div>
          </div>
          <div className="fs-10 text-muted">
            <div>
              ID: <code className="fs-11">{org.id}</code>
            </div>
            <div>
              Owner: <code className="fs-11">{org.ownerUserUUID || '—'}</code>
            </div>
          </div>
        </Card.Body>
      </Card>

      <Card className="shadow-none border">
        <Tab.Container activeKey={tab} onSelect={onTabChange}>
          <Card.Header className="border-bottom border-200 p-0">
            <Nav variant="tabs" className="fs-10">
              <Nav.Item>
                <Nav.Link eventKey="overview">Overview</Nav.Link>
              </Nav.Item>
              <Nav.Item>
                <Nav.Link eventKey="members">Members</Nav.Link>
              </Nav.Item>
              <Nav.Item>
                <Nav.Link eventKey="divisions">Divisions</Nav.Link>
              </Nav.Item>
              <Nav.Item>
                <Nav.Link eventKey="subscriptions">Subscriptions</Nav.Link>
              </Nav.Item>
              <Nav.Item>
                <Nav.Link eventKey="payments">Payments</Nav.Link>
              </Nav.Item>
              <Nav.Item>
                <Nav.Link eventKey="activity">Activity</Nav.Link>
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
                <Tab.Pane eventKey="overview">
                  <OverviewTab org={org} />
                </Tab.Pane>
                <Tab.Pane eventKey="members">
                  <MembersTab org={org} />
                </Tab.Pane>
                <Tab.Pane eventKey="divisions">
                  <DivisionsTab org={org} />
                </Tab.Pane>
                <Tab.Pane eventKey="subscriptions">
                  <SubscriptionsTab org={org} />
                </Tab.Pane>
                <Tab.Pane eventKey="payments">
                  <PaymentsTab org={org} />
                </Tab.Pane>
                <Tab.Pane eventKey="activity">
                  <ActivityTab />
                </Tab.Pane>
              </Tab.Content>
            </Suspense>
          </Card.Body>
        </Tab.Container>
      </Card>
    </>
  );
};

export default ClientDetailPage;
