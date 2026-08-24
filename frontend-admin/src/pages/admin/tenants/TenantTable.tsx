import { useMemo, useState } from 'react';
import { Button, Card, Form, Spinner, Table } from 'react-bootstrap';
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome';
import { toast } from 'react-toastify';
import { Trans, useTranslation } from 'react-i18next';
import SubtleBadge from 'components/common/SubtleBadge';
import type { BadgeColor } from 'components/common/SubtleBadge';
import {
  useSetDefaultTenantMutation,
  type AdminOrgListItem
} from 'store/api/tenantApi';
import TenantTableHeader from './TenantTableHeader';

const planColors: Record<string, BadgeColor> = {
  free: 'secondary',
  pro: 'primary',
  enterprise: 'success'
};

// Eligible for "Set as default": an internal, active, non-soft-deleted
// tenant that isn't already the default. Mirrors the backend's
// TransferDefaultTenant target validation (operational internal tenant) so
// the UI never offers an action the server would 409 on the happy path —
// the mutation itself is still the source of truth for a stale-cache race.
const canSetAsDefault = (org: AdminOrgListItem): boolean =>
  org.kind === 'internal' &&
  org.status === 'active' &&
  !org.deletedAt &&
  !org.isDefault;

function extractError(err: unknown, fallback: string): string {
  if (err && typeof err === 'object' && 'data' in err) {
    const data = (err as { data?: { detail?: string; title?: string } }).data;
    return data?.detail || data?.title || fallback;
  }
  return String(err);
}

interface Props {
  orgs: AdminOrgListItem[];
  isLoading: boolean;
  error: boolean;
  includeDeleted: boolean;
  onIncludeDeletedChange: (value: boolean) => void;
  /** Lifted to the parent so its debounced value can drive the API query. */
  searchTerm: string;
  onSearchChange: (value: string) => void;
  /** Toggle for "include soft-deleted users in member-side hits". Only
   * meaningful and only displayed when a search is active. */
  includeDeletedUsers: boolean;
  onIncludeDeletedUsersChange: (value: boolean) => void;
  /** True when the debounced search term is non-empty — server is filtering
   * results and matchedMembers chips become relevant. */
  searchActive: boolean;
  onRowClick: (org: AdminOrgListItem) => void;
  onCreateClick: () => void;
  onDeleteClick: (org: AdminOrgListItem) => void;
  /** Toolbar heading — see TenantTableHeader.Props.title. */
  title?: string;
  /** "New …" button label — see TenantTableHeader.Props.createLabel. */
  createLabel?: string;
}

const TenantTable: React.FC<Props> = ({
  orgs,
  isLoading,
  error,
  includeDeleted,
  onIncludeDeletedChange,
  searchTerm,
  onSearchChange,
  includeDeletedUsers,
  onIncludeDeletedUsersChange,
  searchActive,
  onRowClick,
  onCreateClick,
  onDeleteClick,
  title,
  createLabel
}) => {
  const { t } = useTranslation();
  const [planFilter, setPlanFilter] = useState('');
  const [setDefaultTenant, { isLoading: isSettingDefault }] =
    useSetDefaultTenantMutation();

  const handleSetDefault = async (org: AdminOrgListItem) => {
    try {
      await setDefaultTenant({ tenantId: org.id }).unwrap();
      toast.success(
        t('adminTenants.default.transferSuccess', { name: org.name })
      );
    } catch (err: unknown) {
      toast.error(
        t('adminTenants.default.transferError', {
          message: extractError(err, t('adminTenants.default.unknownError'))
        })
      );
    }
  };

  // Plan filter stays client-side — the server-side `q` already narrows the
  // tenant list to text/member matches, and the plan dropdown is a small
  // additional chip filter on top of that.
  const filtered = useMemo(() => {
    return orgs.filter(o => {
      if (planFilter && o.plan !== planFilter) return false;
      return true;
    });
  }, [orgs, planFilter]);

  const formatDate = (dateStr?: string | null) => {
    if (!dateStr) return '\u2014';
    const date = new Date(dateStr);
    return date.toLocaleDateString('en-GB', {
      day: '2-digit',
      month: 'short',
      year: 'numeric'
    });
  };

  if (error) {
    return (
      <Card>
        <Card.Body className="text-center text-danger py-5">
          <Trans
            i18nKey="adminTenants.table.errorLoadIntro"
            components={{ code: <code /> }}
          />
        </Card.Body>
      </Card>
    );
  }

  return (
    <Card>
      <Card.Header className="border-bottom border-200 px-4 py-3">
        <TenantTableHeader
          searchTerm={searchTerm}
          onSearchChange={onSearchChange}
          searchActive={searchActive}
          includeDeletedUsers={includeDeletedUsers}
          onIncludeDeletedUsersChange={onIncludeDeletedUsersChange}
          planFilter={planFilter}
          onPlanChange={setPlanFilter}
          includeDeleted={includeDeleted}
          onIncludeDeletedChange={onIncludeDeletedChange}
          onCreateClick={onCreateClick}
          title={title}
          createLabel={createLabel}
        />
      </Card.Header>
      <Card.Body className="p-0">
        {isLoading ? (
          <div className="text-center py-5">
            <Spinner animation="border" size="sm" />
          </div>
        ) : (
          <Table responsive size="sm" className="fs-10 mb-0 overflow-hidden">
            <thead className="bg-body-tertiary">
              <tr>
                <th className="pe-4 ps-3">{t('adminTenants.table.colName')}</th>
                <th>{t('adminTenants.table.colSlug')}</th>
                <th>{t('adminTenants.table.colPlan')}</th>
                <th className="text-center">
                  {t('adminTenants.table.colMembers')}
                </th>
                <th>{t('adminTenants.table.colCreated')}</th>
                <th>{t('adminTenants.table.colStatus')}</th>
                <th className="text-end pe-4">
                  {t('adminTenants.table.colActions')}
                </th>
              </tr>
            </thead>
            <tbody>
              {filtered.map(org => {
                const purged = org.status === 'purged';
                const deleted =
                  !purged && (!!org.deletedAt || org.status === 'archived');
                const statusBadge = purged
                  ? {
                      bg: 'dark' as BadgeColor,
                      label: t('adminTenants.table.statusPurged')
                    }
                  : deleted
                    ? {
                        bg: 'danger' as BadgeColor,
                        label: t('adminTenants.table.statusDeleted')
                      }
                    : {
                        bg: 'success' as BadgeColor,
                        label: t('adminTenants.table.statusActive')
                      };
                return (
                  <tr
                    key={org.id}
                    className="align-middle"
                    style={{
                      cursor: 'pointer',
                      opacity: purged ? 0.4 : deleted ? 0.55 : 1
                    }}
                    onClick={() => onRowClick(org)}
                  >
                    <td className="ps-3 fw-semibold text-900">
                      <div className="d-flex align-items-center gap-2">
                        <span>{org.name}</span>
                        {org.isDefault && (
                          <SubtleBadge
                            bg="primary"
                            pill
                            className="fs-11 fw-normal"
                            data-testid="tenant-default-badge"
                          >
                            {t('adminTenants.default.badge')}
                          </SubtleBadge>
                        )}
                      </div>
                      {searchActive &&
                        org.matchedMembers &&
                        org.matchedMembers.length > 0 && (
                          <div className="mt-1 d-flex flex-wrap gap-1">
                            {org.matchedMembers.map(m => (
                              <span
                                key={m.userUUID}
                                title={[m.fullName, m.username, m.email]
                                  .filter(Boolean)
                                  .join(' · ')}
                              >
                                <SubtleBadge
                                  bg="info"
                                  pill
                                  className="fs-11 fw-normal"
                                >
                                  <FontAwesomeIcon
                                    icon="user"
                                    className="me-1"
                                  />
                                  {m.email || m.fullName || m.username}
                                </SubtleBadge>
                              </span>
                            ))}
                          </div>
                        )}
                    </td>
                    <td className="text-700">
                      <code className="fs-11">{org.slug}</code>
                    </td>
                    <td>
                      <SubtleBadge
                        bg={planColors[org.plan] || 'secondary'}
                        pill
                      >
                        {org.plan}
                      </SubtleBadge>
                    </td>
                    <td className="text-center text-900">{org.memberCount}</td>
                    <td className="text-700">{formatDate(org.createdAt)}</td>
                    <td>
                      <SubtleBadge bg={statusBadge.bg} pill>
                        {statusBadge.label}
                      </SubtleBadge>
                    </td>
                    <td
                      className="text-end pe-4"
                      onClick={e => e.stopPropagation()}
                    >
                      <Button
                        variant="link"
                        size="sm"
                        className="p-0 me-3 text-decoration-none"
                        onClick={() => onRowClick(org)}
                      >
                        {t('adminTenants.table.manage')}
                      </Button>
                      {canSetAsDefault(org) && (
                        <Button
                          variant="link"
                          size="sm"
                          className="p-0 me-3 text-decoration-none"
                          disabled={isSettingDefault}
                          onClick={() => handleSetDefault(org)}
                          title={t('adminTenants.default.setAsDefault')}
                        >
                          <FontAwesomeIcon icon="star" />
                        </Button>
                      )}
                      {org.isDefault ? (
                        <span title={t('adminTenants.default.reassignFirst')}>
                          <FontAwesomeIcon
                            icon="info-circle"
                            className="text-muted"
                          />
                        </span>
                      ) : (
                        !deleted &&
                        !purged && (
                          <Button
                            variant="link"
                            size="sm"
                            className="p-0 text-danger text-decoration-none"
                            onClick={() => onDeleteClick(org)}
                            title={t('adminTenants.table.archiveTitle')}
                          >
                            <FontAwesomeIcon icon="trash" />
                          </Button>
                        )
                      )}
                    </td>
                  </tr>
                );
              })}
              {filtered.length === 0 && (
                <tr>
                  <td colSpan={7} className="text-center text-muted py-4">
                    {t('adminTenants.table.empty')}
                  </td>
                </tr>
              )}
            </tbody>
          </Table>
        )}
      </Card.Body>
      <Card.Footer className="fs-10 text-muted d-flex justify-content-between">
        <span>
          {t(
            orgs.length === 1
              ? 'adminTenants.table.totalOne'
              : 'adminTenants.table.totalOther',
            { count: orgs.length, shown: filtered.length }
          )}
        </span>
        <Form.Check
          type="switch"
          id="tenant-include-deleted"
          label={t('adminTenants.table.includeSoftDeleted')}
          checked={includeDeleted}
          onChange={e => onIncludeDeletedChange(e.target.checked)}
        />
      </Card.Footer>
    </Card>
  );
};

export default TenantTable;
