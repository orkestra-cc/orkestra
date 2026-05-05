import { useMemo, useState } from 'react';
import { useNavigate } from 'react-router';
import { Card, Col, Row } from 'react-bootstrap';
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome';
import type { IconProp } from '@fortawesome/fontawesome-svg-core';
import {
  faBuilding,
  faCircleCheck,
  faUsers,
  faLayerGroup,
} from '@fortawesome/free-solid-svg-icons';
import CountUp from 'react-countup';
import SubtleBadge from 'components/common/SubtleBadge';
import type { BadgeColor } from 'components/common/SubtleBadge';
import { useListAllOrgsAdminQuery, type AdminOrgListItem } from 'store/api/tenantApi';
import TenantTable from './TenantTable';
import TenantDetailModal from './TenantDetailModal';
import CreateTenantModal from './CreateTenantModal';
import DeleteTenantModal from './DeleteTenantModal';
import PurgeTenantModal from './PurgeTenantModal';

// Subtle "Apple-style" stat card: muted label, big animated number, one
// colorful icon chip on the right, optional badge + footnote beneath the
// value. Matches the BillingStatCards pattern used on /billing/dashboard.
interface StatCardProps {
  title: string;
  value: number;
  icon: IconProp;
  accent: 'primary' | 'success' | 'info' | 'warning';
  footnote?: React.ReactNode;
  badge?: { text: string; bg: BadgeColor };
}

const StatCard: React.FC<StatCardProps> = ({
  title,
  value,
  icon,
  accent,
  footnote,
  badge,
}) => (
  <Card className="h-100 shadow-none border">
    <Card.Body>
      <div className="d-flex justify-content-between align-items-start">
        <div>
          <h6 className="text-body-tertiary fs-10 text-uppercase mb-2">
            {title}
            {badge && (
              <SubtleBadge bg={badge.bg} pill className="ms-2 fs-11">
                {badge.text}
              </SubtleBadge>
            )}
          </h6>
          <h3 className="fw-normal text-body mb-0">
            <CountUp start={0} end={value} duration={1.5} separator="," />
          </h3>
        </div>
        <div
          className={`d-flex align-items-center justify-content-center rounded-circle bg-${accent}-subtle`}
          style={{ width: 48, height: 48 }}
        >
          <FontAwesomeIcon icon={icon} className={`fs-5 text-${accent}`} />
        </div>
      </div>
      {footnote && (
        <div className="fs-10 text-body-tertiary mt-3">{footnote}</div>
      )}
    </Card.Body>
  </Card>
);

const planAccent: Record<string, BadgeColor> = {
  free: 'secondary',
  pro: 'primary',
  enterprise: 'success',
};

/**
 * Props for the reusable admin tenant page. The /admin/internal/tenants
 * and /admin/clients pages pass distinct kind + labels so the same
 * component renders as the operator-side or client-side console.
 */
export interface TenantAdminPageProps {
  /** Tenant tier to show — 'internal' or 'external'. Undefined = both. */
  kind?: 'internal' | 'external';
  /** Exclude tenants that have a parent (show root-level only). */
  rootsOnly?: boolean;
  /** Page heading / stat-card labels. */
  labels?: {
    /** Heading shown in the table toolbar (replaces the legacy
     * "Tenant Management" string). */
    toolbarTitle?: string;
    totalTitle?: string;
    activeTitle?: string;
    membersTitle?: string;
    createLabel?: string;
    createTitle?: string;
    createSubmitLabel?: string;
    emptyFootnote?: string;
  };
  /**
   * When set, clicking a row navigates to `${detailPathPrefix}/${orgId}`
   * instead of opening the in-page detail modal. Enables the Phase 4
   * tabbed detail pages at /admin/clients/:id and
   * /admin/internal/tenants/:id. Delete / purge modals still mount in
   * page so destructive actions remain a two-step confirm.
   */
  detailPathPrefix?: string;
}

const TenantManagementPage: React.FC<TenantAdminPageProps> = ({
  kind,
  rootsOnly,
  labels,
  detailPathPrefix,
}) => {
  const navigate = useNavigate();
  const [includeDeleted, setIncludeDeleted] = useState(false);
  const queryArg = {
    includeDeleted: includeDeleted || undefined,
    kind,
    rootsOnly: rootsOnly || undefined,
  };
  // Pass undefined when every filter is false so the RTK Query cache key
  // matches the pre-split "no arg" variant and existing consumers keep
  // their subscriptions.
  const hasFilter = Boolean(
    queryArg.includeDeleted || queryArg.kind || queryArg.rootsOnly,
  );
  const { data, isLoading, error } = useListAllOrgsAdminQuery(
    hasFilter ? queryArg : undefined,
  );

  const [selectedOrg, setSelectedOrg] = useState<AdminOrgListItem | null>(null);
  const [showCreate, setShowCreate] = useState(false);
  const [showDetail, setShowDetail] = useState(false);
  const [pendingDelete, setPendingDelete] = useState<AdminOrgListItem | null>(null);
  const [pendingPurge, setPendingPurge] = useState<AdminOrgListItem | null>(null);

  const stats = useMemo(() => {
    const orgs = data?.tenants ?? [];
    const active = orgs.filter((o) => !o.deletedAt);
    const deleted = orgs.filter((o) => !!o.deletedAt);
    const planBreakdown: Record<string, number> = {};
    let totalMembers = 0;
    for (const o of active) {
      const plan = o.plan || 'free';
      planBreakdown[plan] = (planBreakdown[plan] ?? 0) + 1;
      totalMembers += o.memberCount;
    }
    return {
      total: orgs.length,
      active: active.length,
      deleted: deleted.length,
      totalMembers,
      planBreakdown,
    };
  }, [data]);

  const handleRowClick = (org: AdminOrgListItem) => {
    if (detailPathPrefix) {
      navigate(`${detailPathPrefix}/${org.id}`);
      return;
    }
    setSelectedOrg(org);
    setShowDetail(true);
  };

  const handleDelete = (org: AdminOrgListItem) => {
    setPendingDelete(org);
  };

  return (
    <>
      <Row className="g-3 mb-4">
        <Col md={6} xl={3}>
          <StatCard
            title={labels?.totalTitle ?? 'Total tenants'}
            value={stats.total}
            icon={faBuilding}
            accent="primary"
            footnote={
              stats.deleted > 0
                ? `${stats.active} active · ${stats.deleted} soft-deleted`
                : (labels?.emptyFootnote ?? 'All tenants are active')
            }
          />
        </Col>
        <Col md={6} xl={3}>
          <StatCard
            title="Active"
            value={stats.active}
            icon={faCircleCheck}
            accent="success"
            badge={
              stats.deleted > 0
                ? { text: `${stats.deleted} deleted`, bg: 'warning' }
                : undefined
            }
            footnote={
              stats.total > 0
                ? `${Math.round((stats.active / stats.total) * 100)}% of total`
                : '—'
            }
          />
        </Col>
        <Col md={6} xl={3}>
          <StatCard
            title="Members"
            value={stats.totalMembers}
            icon={faUsers}
            accent="info"
            footnote={
              stats.active > 0
                ? `${(stats.totalMembers / stats.active).toFixed(1)} avg per tenant`
                : 'No tenants yet'
            }
          />
        </Col>
        <Col md={6} xl={3}>
          <Card className="h-100 shadow-none border">
            <Card.Body>
              <div className="d-flex justify-content-between align-items-start">
                <div>
                  <h6 className="text-body-tertiary fs-10 text-uppercase mb-2">
                    Plan mix
                  </h6>
                  <h3 className="fw-normal text-body mb-0">
                    {Object.keys(stats.planBreakdown).length || '—'}
                    <span className="fs-9 text-body-tertiary fw-normal ms-2">
                      {Object.keys(stats.planBreakdown).length === 1 ? 'plan' : 'plans'}
                    </span>
                  </h3>
                </div>
                <div
                  className="d-flex align-items-center justify-content-center rounded-circle bg-warning-subtle"
                  style={{ width: 48, height: 48 }}
                >
                  <FontAwesomeIcon icon={faLayerGroup} className="fs-5 text-warning" />
                </div>
              </div>
              <div className="d-flex flex-wrap gap-1 mt-3">
                {Object.entries(stats.planBreakdown).length === 0 ? (
                  <span className="fs-10 text-body-tertiary">
                    No active tenants
                  </span>
                ) : (
                  Object.entries(stats.planBreakdown)
                    .sort((a, b) => b[1] - a[1])
                    .map(([plan, count]) => (
                      <SubtleBadge
                        key={plan}
                        bg={planAccent[plan] || 'secondary'}
                        pill
                        className="fs-11"
                      >
                        {plan} · {count}
                      </SubtleBadge>
                    ))
                )}
              </div>
            </Card.Body>
          </Card>
        </Col>
      </Row>

      <TenantTable
        orgs={data?.tenants ?? []}
        isLoading={isLoading}
        error={!!error}
        includeDeleted={includeDeleted}
        onIncludeDeletedChange={setIncludeDeleted}
        onRowClick={handleRowClick}
        onCreateClick={() => setShowCreate(true)}
        onDeleteClick={handleDelete}
        title={labels?.toolbarTitle}
        createLabel={labels?.createLabel}
      />

      <CreateTenantModal
        show={showCreate}
        onHide={() => setShowCreate(false)}
        kind={kind}
        title={labels?.createTitle}
        submitLabel={labels?.createSubmitLabel}
      />

      <TenantDetailModal
        org={selectedOrg}
        show={showDetail}
        onHide={() => setShowDetail(false)}
        onDelete={(o) => {
          setShowDetail(false);
          setPendingDelete(o);
        }}
        onPurge={(o) => {
          setShowDetail(false);
          setPendingPurge(o);
        }}
      />

      <DeleteTenantModal
        org={pendingDelete}
        show={!!pendingDelete}
        onHide={() => setPendingDelete(null)}
      />

      <PurgeTenantModal
        org={pendingPurge}
        show={!!pendingPurge}
        onHide={() => setPendingPurge(null)}
      />
    </>
  );
};

export default TenantManagementPage;
