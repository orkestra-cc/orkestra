import { useState, type FormEvent } from 'react';
import { Card, Col, Form, Row, Spinner } from 'react-bootstrap';
import { faGavel } from '@fortawesome/free-solid-svg-icons';
import type { CellContext, ColumnDef } from '@tanstack/react-table';
import { toast } from 'react-toastify';
import IconButton from 'components/common/IconButton';
import SubtleBadge from 'components/common/SubtleBadge';
import {
  useListLegalHoldsQuery,
  usePlaceLegalHoldMutation,
  useReleaseLegalHoldMutation,
  type LegalHold
} from 'store/api/complianceApi';
import ComplianceEmptyState from './ComplianceEmptyState';
import SectionCard from 'components/common/SectionCard';
import ComplianceTable from './ComplianceTable';
import { formatDateTime } from './complianceFormat';

// LegalHoldsTab lets operators place a litigation hold on a subject (which
// blocks erasure) and release it. Place + release are step-up-gated on the
// backend; the global StepUpModal replays the request after re-auth.
const LegalHoldsTab = () => {
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
      toast.success('Legal hold placed');
      setUserUuid('');
      setReason('');
      setCaseRef('');
    } catch {
      toast.error('Place failed');
    }
  };
  const onRelease = async (id: string) => {
    try {
      await release({
        id,
        releaseReason: 'released via admin console'
      }).unwrap();
      toast.success('Legal hold released');
    } catch {
      toast.error('Release failed');
    }
  };

  const columns: ColumnDef<LegalHold>[] = [
    {
      accessorKey: 'userUuid',
      header: 'Subject',
      meta: { headerProps: { className: 'text-900' } },
      cell: ({ row: { original } }: CellContext<LegalHold, unknown>) => (
        <span className="font-monospace small">{original.userUuid}</span>
      )
    },
    {
      accessorKey: 'reason',
      header: 'Reason',
      meta: { headerProps: { className: 'text-900' } }
    },
    {
      accessorKey: 'caseRef',
      header: 'Case',
      meta: { headerProps: { className: 'text-900' } },
      cell: ({ row: { original } }: CellContext<LegalHold, unknown>) =>
        original.caseRef || '—'
    },
    {
      accessorKey: 'active',
      header: 'Status',
      meta: { headerProps: { className: 'text-900' } },
      cell: ({ row: { original } }: CellContext<LegalHold, unknown>) => (
        <SubtleBadge pill bg={original.active ? 'warning' : 'secondary'}>
          {original.active ? 'Active' : 'Released'}
        </SubtleBadge>
      )
    },
    {
      accessorKey: 'placedAt',
      header: 'Placed',
      meta: { headerProps: { className: 'text-900' } },
      cell: ({ row: { original } }: CellContext<LegalHold, unknown>) =>
        formatDateTime(original.placedAt)
    },
    {
      id: 'actions',
      header: 'Actions',
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
            Release
          </IconButton>
        ) : (
          <span className="text-400">—</span>
        )
    }
  ];

  const items = data?.items ?? [];

  return (
    <SectionCard icon={faGavel} iconColor="danger" title="Legal Holds">
      <Card className="bg-body-tertiary border shadow-none mb-4">
        <Card.Body className="py-3">
          <Form onSubmit={onPlace}>
            <Row className="g-2 align-items-end">
              <Col md={4}>
                <Form.Label className="fs-11 text-700 mb-1">
                  Subject userUuid
                </Form.Label>
                <Form.Control
                  size="sm"
                  placeholder="e.g. 7f3c…"
                  value={userUuid}
                  onChange={e => setUserUuid(e.target.value)}
                />
              </Col>
              <Col md={4}>
                <Form.Label className="fs-11 text-700 mb-1">Reason</Form.Label>
                <Form.Control
                  size="sm"
                  placeholder="Litigation hold reason"
                  value={reason}
                  onChange={e => setReason(e.target.value)}
                />
              </Col>
              <Col md={2}>
                <Form.Label className="fs-11 text-700 mb-1">
                  Case ref
                </Form.Label>
                <Form.Control
                  size="sm"
                  placeholder="Optional"
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
                  Place hold
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
          message="No active legal holds."
          hint="Place a hold above to block erasure of a subject under litigation."
        />
      ) : (
        <ComplianceTable
          data={items}
          columns={columns}
          searchPlaceholder="Search by subject, reason or case"
        />
      )}
    </SectionCard>
  );
};

export default LegalHoldsTab;
