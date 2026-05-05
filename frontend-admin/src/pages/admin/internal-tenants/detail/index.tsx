import { Suspense, lazy, useMemo } from 'react';
import { Link, Navigate, useParams, useSearchParams } from 'react-router';
import { Alert, Breadcrumb, Card, Nav, Spinner, Tab } from 'react-bootstrap';
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome';
import SubtleBadge from 'components/common/SubtleBadge';
import type { BadgeColor } from 'components/common/SubtleBadge';
import { useGetOrgAdminQuery } from 'store/api/tenantApi';

// Reuse the Overview and Members tabs from the clients detail folder —
// they only take an `org` prop and are equally valid for internal
// tenants. The Divisions/Subscriptions/Payments tabs stay client-only.
const OverviewTab = lazy(() => import('pages/admin/clients/detail/OverviewTab'));
const MembersTab = lazy(() => import('pages/admin/clients/detail/MembersTab'));

const TAB_KEYS = ['overview', 'members'] as const;
type TabKey = (typeof TAB_KEYS)[number];
const DEFAULT_TAB: TabKey = 'overview';

function readTab(param: string | null): TabKey {
  const candidate = (param ?? DEFAULT_TAB) as TabKey;
  return TAB_KEYS.includes(candidate) ? candidate : DEFAULT_TAB;
}

const planColors: Record<string, BadgeColor> = {
  free: 'secondary',
  pro: 'primary',
  enterprise: 'success',
};

const InternalTenantDetailPage: React.FC = () => {
  const { tenantId } = useParams<{ tenantId: string }>();
  const [searchParams, setSearchParams] = useSearchParams();
  const tab = readTab(searchParams.get('tab'));

  const { data: org, isLoading, error } = useGetOrgAdminQuery(tenantId ?? '', {
    skip: !tenantId,
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

  if (!tenantId) {
    return <Navigate to="/admin/internal/tenants" replace />;
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
        Tenant not found or you lack permission to view it.{' '}
        <Link to="/admin/internal/tenants">Back to internal tenants</Link>
      </Alert>
    );
  }

  // External-tenant deep links land on the Client detail page — this
  // page is operator-side only.
  if (org.kind === 'external') {
    return <Navigate to={`/admin/clients/${tenantId}`} replace />;
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
        <Breadcrumb.Item linkAs={Link} linkProps={{ to: '/admin/internal/tenants' }}>
          Internal Tenants
        </Breadcrumb.Item>
        <Breadcrumb.Item active>{org.name}</Breadcrumb.Item>
      </Breadcrumb>

      <Card className="mb-3 shadow-none border">
        <Card.Body className="d-flex justify-content-between align-items-start flex-wrap gap-3">
          <div>
            <h3 className="fw-normal mb-1">
              <FontAwesomeIcon icon="building" className="text-primary me-2" />
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
              <SubtleBadge bg="primary" pill>
                internal
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
              </Tab.Content>
            </Suspense>
          </Card.Body>
        </Tab.Container>
      </Card>
    </>
  );
};

export default InternalTenantDetailPage;
