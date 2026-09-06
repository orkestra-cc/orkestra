import { useState, type FormEvent } from 'react';
import { Card, Col, Form, Row, Spinner } from 'react-bootstrap';
import { faGavel } from '@fortawesome/free-solid-svg-icons';
import type { CellContext, ColumnDef } from '@tanstack/react-table';
import { useTranslation } from 'react-i18next';
import { toast } from 'react-toastify';
import IconButton from 'components/common/IconButton';
import SubtleBadge from 'components/common/SubtleBadge';
import { byTimestamp } from 'components/common/advance-table/sorting';
import {
  useListLegalHoldsQuery,
  usePlaceLegalHoldMutation,
  useReleaseLegalHoldMutation,
  type LegalHold
} from 'store/api/complianceApi';
import ComplianceEmptyState from './ComplianceEmptyState';
import ComplianceTable from './ComplianceTable';
import { formatDateTime } from './complianceFormat';

// LegalHoldsTab lets operators place a litigation hold on a subject (which
// blocks erasure) and release it. Place + release are step-up-gated on the
// backend; the global StepUpModal replays the request after re-auth.
const LegalHoldsTab = () => {
  const { t } = useTranslation();
  const { data, isLoading } = useListLegalHoldsQuery();
  const [place, { isLoading: isPlacing }] = usePlaceLegalHoldMutation();
  const [release] = useReleaseLegalHoldMutation();
  const [userUuid, setUserUuid] = useState('');
  const [reason, setReason] = useState('');
  const [caseRef, setCaseRef] = useState('');

  const onPlace = async (e: FormEvent) => {
    e.preventDefault();
    if (!userUuid || !reason) return;
    try {
      await place({ userUuid, reason, caseRef: caseRef || undefined }).unwrap();
      toast.success(t('adminCompliance.holds.placeSuccess'));
      setUserUuid('');
      setReason('');
      setCaseRef('');
    } catch {
      toast.error(t('adminCompliance.holds.placeError'));
    }
  };
  const onRelease = async (id: string) => {
    try {
      await release({
        id,
        releaseReason: 'released via admin console'
      }).unwrap();
      toast.success(t('adminCompliance.holds.releaseSuccess'));
    } catch {
      toast.error(t('adminCompliance.holds.releaseError'));
    }
  };

  const columns: ColumnDef<LegalHold>[] = [
    {
      accessorKey: 'userUuid',
      header: t('adminCompliance.holds.columns.subject'),
      meta: { headerProps: { className: 'text-900' } },
      cell: ({ row: { original } }: CellContext<LegalHold, unknown>) => (
        <span className="font-monospace small">{original.userUuid}</span>
      )
    },
    {
      accessorKey: 'reason',
      header: t('adminCompliance.holds.columns.reason'),
      meta: { headerProps: { className: 'text-900' } }
    },
    {
      accessorKey: 'caseRef',
      header: t('adminCompliance.holds.columns.case'),
      meta: { headerProps: { className: 'text-900' } },
      cell: ({ row: { original } }: CellContext<LegalHold, unknown>) =>
        original.caseRef || '—'
    },
    {
      accessorKey: 'active',
      header: t('adminCompliance.holds.columns.status'),
      meta: { headerProps: { className: 'text-900' } },
      cell: ({ row: { original } }: CellContext<LegalHold, unknown>) => (
        <SubtleBadge pill bg={original.active ? 'warning' : 'secondary'}>
          {original.active
            ? t('adminCompliance.status.active')
            : t('adminCompliance.status.released')}
        </SubtleBadge>
      )
    },
    {
      id: 'placedAt',
      // Formatted accessor + timestamp comparator — see byTimestamp.
      accessorFn: h => formatDateTime(h.placedAt),
      sortingFn: byTimestamp<LegalHold>(h => h.placedAt),
      header: t('adminCompliance.holds.columns.placed'),
      meta: { headerProps: { className: 'text-900' } },
      cell: ({ row: { original } }: CellContext<LegalHold, unknown>) =>
        formatDateTime(original.placedAt)
    },
    {
      id: 'actions',
      header: t('adminCompliance.holds.columns.actions'),
      enableSorting: false,
      meta: {
        headerProps: { className: 'text-end text-900' },
        cellProps: { className: 'text-end' }
      },
      cell: ({ row: { original } }: CellContext<LegalHold, unknown>) =>
        original.active ? (
          <IconButton
            size="sm"
            variant="outline-secondary"
            icon="unlock-alt"
            onClick={() => onRelease(original.uuid)}
          >
            {t('adminCompliance.holds.release')}
          </IconButton>
        ) : (
          <span className="text-400">—</span>
        )
    }
  ];

  const items = data?.items ?? [];

  return (
    <>
      <Card className="bg-body-tertiary border shadow-none mb-4">
        <Card.Body className="py-3">
          <Form onSubmit={onPlace}>
            <Row className="g-2 align-items-end">
              <Col md={4}>
                <Form.Label className="fs-11 text-700 mb-1">
                  {t('adminCompliance.holds.form.subjectLabel')}
                </Form.Label>
                <Form.Control
                  size="sm"
                  placeholder={t(
                    'adminCompliance.holds.form.subjectPlaceholder'
                  )}
                  value={userUuid}
                  onChange={e => setUserUuid(e.target.value)}
                />
              </Col>
              <Col md={4}>
                <Form.Label className="fs-11 text-700 mb-1">
                  {t('adminCompliance.holds.form.reasonLabel')}
                </Form.Label>
                <Form.Control
                  size="sm"
                  placeholder={t(
                    'adminCompliance.holds.form.reasonPlaceholder'
                  )}
                  value={reason}
                  onChange={e => setReason(e.target.value)}
                />
              </Col>
              <Col md={2}>
                <Form.Label className="fs-11 text-700 mb-1">
                  {t('adminCompliance.holds.form.caseRefLabel')}
                </Form.Label>
                <Form.Control
                  size="sm"
                  placeholder={t(
                    'adminCompliance.holds.form.caseRefPlaceholder'
                  )}
                  value={caseRef}
                  onChange={e => setCaseRef(e.target.value)}
                />
              </Col>
              <Col md={2}>
                <IconButton
                  size="sm"
                  type="submit"
                  variant="primary"
                  icon={faGavel}
                  className="w-100 text-nowrap"
                  disabled={isPlacing || !userUuid || !reason}
                >
                  {t('adminCompliance.holds.form.submit')}
                </IconButton>
              </Col>
            </Row>
          </Form>
        </Card.Body>
      </Card>

      {isLoading ? (
        <Spinner animation="border" size="sm" className="mt-2" />
      ) : items.length === 0 ? (
        <ComplianceEmptyState
          icon={faGavel}
          message={t('adminCompliance.holds.emptyMessage')}
          hint={t('adminCompliance.holds.emptyHint')}
        />
      ) : (
        <ComplianceTable
          data={items}
          columns={columns}
          searchPlaceholder={t('adminCompliance.holds.searchPlaceholder')}
        />
      )}
    </>
  );
};

export default LegalHoldsTab;
