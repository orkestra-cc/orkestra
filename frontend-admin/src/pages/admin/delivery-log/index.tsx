// «Registro delle consegne» — the delivery log of the notification module:
// every email the platform tried to send in the last 90 days (the rows'
// TTL), with its outcome. It is the one place an operator sees that a
// message was suppressed (address marked invalid, marketing opt-out, a
// category preference) or why a send failed.
//
// Read-only. GET /v1/notifications needs notification.log.read, a system
// permission (administrator, developer outside production). The log is
// platform-wide and holds recipient addresses: the page shows only what the
// API already returns to that permission, and fewer fields than it returns.
//
// Filters, page and page size live in the URL (shareable, survive a reload).

import { useMemo } from 'react';
import { Alert, Button, Card, Col, Form, Row, Spinner } from 'react-bootstrap';
import { useTranslation } from 'react-i18next';
import { useSearchParams } from 'react-router';
import type {
  ColumnDef,
  OnChangeFn,
  PaginationState
} from '@tanstack/react-table';

import AdvanceTable from 'components/common/advance-table/AdvanceTable';
import AdvanceTableFooter from 'components/common/advance-table/AdvanceTableFooter';
import IconButton from 'components/common/IconButton';
import SubtleBadge, { type BadgeColor } from 'components/common/SubtleBadge';
import { formatDateTime } from 'helpers/dateFormat';
import useAdvanceTable from 'hooks/ui/useAdvanceTable';
import useDebounce from 'hooks/useDebounce';
import AdvanceTableProvider from 'providers/AdvanceTableProvider';
import { useListDeliveryLogQuery } from 'store/api/notificationLogApi';
import type {
  DeliveryLogEntry,
  DeliveryStatus,
  DeliveryType
} from 'types/notification';

const P = 'adminDeliveryLog';
const STATUSES: DeliveryStatus[] = ['sent', 'suppressed', 'failed', 'queued'];
const TYPES: DeliveryType[] = ['transactional', 'marketing'];

const STATUS_BADGE: Record<string, BadgeColor> = {
  sent: 'success',
  suppressed: 'warning',
  failed: 'danger',
  queued: 'secondary'
};

// The suppression causes the notification module writes into `error`
// (notification_service.go). Anything else on a suppressed or failed row is
// the module's bounded diagnostic, shown as it is.
const REASON_KEYS: Record<string, string> = {
  address_invalid: 'addressInvalid',
  marketing_optout: 'marketingOptout',
  'user opted out of category': 'categoryPreference'
};

const positiveInt = (value: string | null, fallback: number) => {
  const parsed = Number(value);
  return Number.isInteger(parsed) && parsed > 0 ? parsed : fallback;
};

const DeliveryLogPage: React.FC = () => {
  const { t } = useTranslation();
  const [searchParams, setSearchParams] = useSearchParams();

  const address = searchParams.get('q') ?? '';
  const debouncedAddress = useDebounce(address.trim(), 300);
  const category = searchParams.get('category') ?? '';
  const debouncedCategory = useDebounce(category.trim(), 300);
  const statusParam = searchParams.get('status');
  const status = STATUSES.includes(statusParam as DeliveryStatus)
    ? (statusParam as DeliveryStatus)
    : undefined;
  const typeParam = searchParams.get('type');
  const type = TYPES.includes(typeParam as DeliveryType)
    ? (typeParam as DeliveryType)
    : undefined;
  const page = positiveInt(searchParams.get('page'), 1);
  const pageSize = Math.min(positiveInt(searchParams.get('pageSize'), 25), 100);

  const updateParams = (changes: Record<string, string | null>) => {
    setSearchParams(
      previous => {
        const next = new URLSearchParams(previous);
        Object.entries(changes).forEach(([key, value]) => {
          if (!value) next.delete(key);
          else next.set(key, value);
        });
        return next;
      },
      { replace: true }
    );
  };

  const { data, isLoading, isFetching, isError, refetch } =
    useListDeliveryLogQuery({
      status,
      type,
      category: debouncedCategory || undefined,
      address: debouncedAddress || undefined,
      skip: (page - 1) * pageSize,
      limit: pageSize
    });
  const items = useMemo(() => data?.items ?? [], [data]);
  const total = data?.total ?? 0;

  // Categories seen on this page, offered as suggestions: the filter is an
  // exact match and the codes are not something an operator remembers.
  const categories = useMemo(
    () => Array.from(new Set(items.map(item => item.category))).sort(),
    [items]
  );

  const reasonCell = (e: DeliveryLogEntry) => {
    if (!e.error) return <span className="text-muted">—</span>;
    const key = REASON_KEYS[e.error];
    if (key) return t(`${P}.reason.${key}`);
    return <code className="fs-11 text-break">{e.error}</code>;
  };

  const columns: ColumnDef<DeliveryLogEntry>[] = [
    {
      id: 'createdAt',
      header: t(`${P}.col.date`),
      cell: ({ row }) => (
        <span className="text-nowrap">
          {formatDateTime(row.original.createdAt)}
        </span>
      ),
      meta: {
        headerProps: { className: 'text-900 ps-3' },
        cellProps: { className: 'ps-3' }
      }
    },
    {
      id: 'type',
      header: t(`${P}.col.type`),
      cell: ({ row }) =>
        t(`${P}.type.${row.original.type}`, {
          defaultValue: row.original.type
        }),
      meta: {
        headerProps: { className: 'text-900 d-none d-md-table-cell' },
        cellProps: { className: 'd-none d-md-table-cell' }
      }
    },
    {
      id: 'category',
      header: t(`${P}.col.category`),
      cell: ({ row }) => (
        <code className="fs-11 text-break">{row.original.category}</code>
      ),
      meta: {
        headerProps: { className: 'text-900 d-none d-lg-table-cell' },
        cellProps: { className: 'd-none d-lg-table-cell' }
      }
    },
    {
      id: 'recipient',
      header: t(`${P}.col.recipient`),
      cell: ({ row }) => (
        <span className="text-900 text-break">
          {row.original.recipientAddress || '—'}
        </span>
      )
    },
    {
      id: 'status',
      header: t(`${P}.col.status`),
      cell: ({ row }) => (
        <SubtleBadge bg={STATUS_BADGE[row.original.status] ?? 'secondary'} pill>
          {t(`${P}.status.${row.original.status}`, {
            defaultValue: row.original.status
          })}
        </SubtleBadge>
      )
    },
    {
      id: 'reason',
      header: t(`${P}.col.reason`),
      cell: ({ row }) => reasonCell(row.original)
    },
    {
      id: 'sender',
      header: t(`${P}.col.sender`),
      cell: ({ row }) =>
        row.original.senderSlug ? (
          <code className="fs-11">{row.original.senderSlug}</code>
        ) : (
          <span className="text-muted">—</span>
        ),
      meta: {
        headerProps: { className: 'text-900 d-none d-xl-table-cell pe-3' },
        cellProps: { className: 'd-none d-xl-table-cell pe-3' }
      }
    }
  ];

  const pagination = useMemo<PaginationState>(
    () => ({ pageIndex: page - 1, pageSize }),
    [page, pageSize]
  );
  const onPaginationChange: OnChangeFn<PaginationState> = updater => {
    const next = typeof updater === 'function' ? updater(pagination) : updater;
    updateParams({
      page: String(next.pageSize === pageSize ? next.pageIndex + 1 : 1),
      pageSize: String(next.pageSize)
    });
  };

  const table = useAdvanceTable<DeliveryLogEntry>({
    data: items,
    columns,
    sortable: false,
    pagination: true,
    perPage: pageSize,
    manualPagination: true,
    rowCount: total,
    state: { pagination },
    onPaginationChange,
    getRowId: row => row.id
  });

  const filtered = !!(debouncedAddress || debouncedCategory || status || type);

  return (
    <>
      <div className="mb-3">
        <h3 className="fw-normal mb-1">{t(`${P}.title`)}</h3>
        <p className="fs-10 text-muted mb-0">{t(`${P}.subtitle`)}</p>
      </div>

      <AdvanceTableProvider {...table}>
        <Card>
          <Card.Body className="p-0">
            <Row className="g-2 px-x1 py-3 border-bottom border-200 align-items-end">
              <Col xs={12} md={4}>
                <Form.Control
                  size="sm"
                  type="search"
                  value={address}
                  placeholder={t(`${P}.filter.address`)}
                  aria-label={t(`${P}.filter.address`)}
                  onChange={e =>
                    updateParams({ q: e.target.value || null, page: '1' })
                  }
                />
              </Col>
              <Col xs={6} md={2}>
                <Form.Select
                  size="sm"
                  value={status ?? ''}
                  aria-label={t(`${P}.filter.status`)}
                  onChange={e =>
                    updateParams({ status: e.target.value || null, page: '1' })
                  }
                >
                  <option value="">{t(`${P}.filter.anyStatus`)}</option>
                  {STATUSES.map(s => (
                    <option key={s} value={s}>
                      {t(`${P}.status.${s}`)}
                    </option>
                  ))}
                </Form.Select>
              </Col>
              <Col xs={6} md={2}>
                <Form.Select
                  size="sm"
                  value={type ?? ''}
                  aria-label={t(`${P}.filter.type`)}
                  onChange={e =>
                    updateParams({ type: e.target.value || null, page: '1' })
                  }
                >
                  <option value="">{t(`${P}.filter.anyType`)}</option>
                  {TYPES.map(ty => (
                    <option key={ty} value={ty}>
                      {t(`${P}.type.${ty}`)}
                    </option>
                  ))}
                </Form.Select>
              </Col>
              <Col xs={10} md={3}>
                <Form.Control
                  size="sm"
                  type="search"
                  list="delivery-log-categories"
                  value={category}
                  placeholder={t(`${P}.filter.category`)}
                  aria-label={t(`${P}.filter.category`)}
                  onChange={e =>
                    updateParams({
                      category: e.target.value || null,
                      page: '1'
                    })
                  }
                />
                <datalist id="delivery-log-categories">
                  {categories.map(c => (
                    <option key={c} value={c} />
                  ))}
                </datalist>
              </Col>
              <Col xs="auto" className="ms-md-auto">
                <IconButton
                  icon="sync-alt"
                  variant="outline-secondary"
                  size="sm"
                  aria-label={t(`${P}.refresh`)}
                  title={t(`${P}.refresh`)}
                  disabled={isFetching}
                  onClick={() => refetch()}
                />
              </Col>
            </Row>
            {isLoading ? (
              <div className="p-4 text-center text-muted">
                <Spinner size="sm" className="me-2" />
                {t(`${P}.loading`)}
              </div>
            ) : isError ? (
              <Alert variant="danger" className="m-3">
                <div className="d-flex justify-content-between align-items-center gap-3">
                  <span>{t(`${P}.loadError`)}</span>
                  <Button
                    variant="outline-danger"
                    size="sm"
                    onClick={() => refetch()}
                  >
                    {t(`${P}.retry`)}
                  </Button>
                </div>
              </Alert>
            ) : items.length === 0 ? (
              <div className="p-4 text-center text-muted">
                {filtered ? t(`${P}.noResults`) : t(`${P}.empty`)}
              </div>
            ) : (
              <AdvanceTable
                headerClassName="align-middle"
                rowClassName="align-middle"
                tableProps={{
                  size: 'sm',
                  className: 'fs-10 mb-0 overflow-hidden'
                }}
              />
            )}
            {items.length > 0 && (
              <div className="px-x1 py-2 border-top border-200">
                <AdvanceTableFooter
                  rowInfo
                  navButtons
                  rowsPerPageSelection
                  rowsPerPageOptions={[10, 25, 50, 100]}
                />
              </div>
            )}
          </Card.Body>
        </Card>
      </AdvanceTableProvider>
    </>
  );
};

export default DeliveryLogPage;
