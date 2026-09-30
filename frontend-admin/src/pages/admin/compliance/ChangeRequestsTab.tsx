import { useState } from 'react';
import { Col, Form, Row, Spinner } from 'react-bootstrap';
import { useSearchParams } from 'react-router';
import { faEye, faUserCheck } from '@fortawesome/free-solid-svg-icons';
import type { CellContext, ColumnDef } from '@tanstack/react-table';
import { useTranslation } from 'react-i18next';
import IconButton from 'components/common/IconButton';
import SubtleBadge, { type BadgeColor } from 'components/common/SubtleBadge';
import { byTimestamp } from 'components/common/advance-table/sorting';
import {
  useListChangeRequestsQuery,
  useListCompliancePoliciesQuery,
  type ChangeRequest,
  type ChangeRequestFilter
} from 'store/api/complianceApi';
import ChangeRequestModal from './ChangeRequestModal';
import ComplianceEmptyState from './ComplianceEmptyState';
import ComplianceTable from './ComplianceTable';
import { formatDateTime } from './complianceFormat';

const FILTERS: ChangeRequestFilter[] = [
  'pending',
  'approved',
  'rejected',
  'superseded',
  'expired',
  'all'
];

const statusColor = (s: ChangeRequest['status']): BadgeColor =>
  s === 'pending'
    ? 'warning'
    : s === 'approved'
      ? 'success'
      : s === 'rejected'
        ? 'danger'
        : 'secondary';

// ChangeRequestsTab lists the four-eyes requests (spec §8); the status filter
// lives in ?crStatus= so a link to the pending queue is shareable.
const ChangeRequestsTab = () => {
  const { t } = useTranslation();
  const [searchParams, setSearchParams] = useSearchParams();
  const raw = searchParams.get('crStatus') as ChangeRequestFilter | null;
  const status: ChangeRequestFilter =
    raw && FILTERS.includes(raw) ? raw : 'pending';
  const { data, isLoading } = useListChangeRequestsQuery(status);
  const { data: policies } = useListCompliancePoliciesQuery();
  const [selected, setSelected] = useState<ChangeRequest | null>(null);
  const policyName = (id?: string) =>
    policies?.items.find(p => p.uuid === id)?.name ?? id ?? '—';

  const setStatus = (next: string) =>
    setSearchParams(
      prev => {
        prev.set('crStatus', next);
        return prev;
      },
      { replace: true }
    );

  const columns: ColumnDef<ChangeRequest>[] = [
    {
      id: 'kind',
      accessorFn: cr => t(`adminCompliance.changeRequests.kinds.${cr.kind}`),
      header: t('adminCompliance.changeRequests.columns.kind'),
      meta: { headerProps: { className: 'text-900' } }
    },
    {
      id: 'target',
      accessorFn: cr =>
        cr.kind === 'create'
          ? (cr.payload.policy?.name ?? '—')
          : cr.tenantId
            ? `${cr.tenantId} → ${policyName(cr.payload.policyUuid)}`
            : policyName(cr.policyUuid),
      header: t('adminCompliance.changeRequests.columns.target'),
      meta: { headerProps: { className: 'text-900' } }
    },
    {
      id: 'warnings',
      accessorFn: cr => cr.warnings.length,
      header: t('adminCompliance.changeRequests.columns.warnings'),
      meta: { headerProps: { className: 'text-900' } }
    },
    {
      accessorKey: 'requestedBy',
      header: t('adminCompliance.changeRequests.columns.requestedBy'),
      meta: { headerProps: { className: 'text-900' } },
      cell: ({ row: { original } }: CellContext<ChangeRequest, unknown>) => (
        <span className="font-monospace small">{original.requestedBy}</span>
      )
    },
    {
      id: 'requestedAt',
      accessorFn: cr => formatDateTime(cr.requestedAt),
      sortingFn: byTimestamp<ChangeRequest>(cr => cr.requestedAt),
      header: t('adminCompliance.changeRequests.columns.requestedAt'),
      meta: { headerProps: { className: 'text-900' } }
    },
    {
      accessorKey: 'status',
      header: t('adminCompliance.changeRequests.columns.status'),
      meta: { headerProps: { className: 'text-900' } },
      cell: ({ row: { original } }: CellContext<ChangeRequest, unknown>) => (
        <SubtleBadge pill bg={statusColor(original.status)}>
          {t(`adminCompliance.changeRequests.statuses.${original.status}`)}
        </SubtleBadge>
      )
    },
    {
      id: 'actions',
      header: t('adminCompliance.changeRequests.columns.actions'),
      enableSorting: false,
      meta: {
        headerProps: { className: 'text-end text-900' },
        cellProps: { className: 'text-end' }
      },
      cell: ({ row: { original } }: CellContext<ChangeRequest, unknown>) => (
        <IconButton
          size="sm"
          variant="outline-secondary"
          icon={faEye}
          onClick={() => setSelected(original)}
        >
          {t('adminCompliance.changeRequests.open')}
        </IconButton>
      )
    }
  ];

  const items = data?.items ?? [];
  return (
    <>
      <Row className="g-2 mb-3 align-items-center">
        <Col xs="auto">
          <Form.Label htmlFor="cr-status" className="fs-11 text-700 mb-0">
            {t('adminCompliance.changeRequests.filterLabel')}
          </Form.Label>
        </Col>
        <Col xs="auto">
          <Form.Select
            id="cr-status"
            size="sm"
            value={status}
            onChange={e => setStatus(e.target.value)}
          >
            {FILTERS.map(f => (
              <option key={f} value={f}>
                {t(`adminCompliance.changeRequests.statuses.${f}`)}
              </option>
            ))}
          </Form.Select>
        </Col>
      </Row>
      {isLoading ? (
        <Spinner animation="border" size="sm" className="mt-2" />
      ) : items.length === 0 ? (
        <ComplianceEmptyState
          icon={faUserCheck}
          message={t('adminCompliance.changeRequests.emptyMessage')}
          hint={t('adminCompliance.changeRequests.emptyHint')}
        />
      ) : (
        <ComplianceTable
          data={items}
          columns={columns}
          searchPlaceholder={t(
            'adminCompliance.changeRequests.searchPlaceholder'
          )}
        />
      )}
      <ChangeRequestModal request={selected} onHide={() => setSelected(null)} />
    </>
  );
};

export default ChangeRequestsTab;
