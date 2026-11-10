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
  useUpdateLegalHoldMutation,
  useAddLegalHoldNoteMutation,
  type LegalHold
} from 'store/api/complianceApi';
import EmptyState from 'components/common/EmptyState';
import SearchablePagedTable from 'components/common/advance-table/SearchablePagedTable';
import { formatDate, formatDateTime } from './complianceFormat';
import { erasureErrorMessage } from './erasureErrors';
import ReasonModal from './policies/ReasonModal';
import LegalHoldEditModal, { type LegalHoldPatch } from './LegalHoldEditModal';
import {
  expiryDayToInstant,
  expiryDisplayInstant,
  reviewDayToInstant
} from './legalHoldDates';
import LegalHoldNotesModal from './LegalHoldNotesModal';

// LegalHoldsTab lets operators place a litigation hold on a subject (which
// blocks erasure) and release it. Place + release are step-up-gated on the
// backend; the global StepUpModal replays the request after re-auth.
const LegalHoldsTab = () => {
  const { t } = useTranslation();
  const [includeReleased, setIncludeReleased] = useState(false);
  const { data, isLoading } = useListLegalHoldsQuery({ includeReleased });
  const [place, { isLoading: isPlacing }] = usePlaceLegalHoldMutation();
  const [release, { isLoading: releasing }] = useReleaseLegalHoldMutation();
  const [update, { isLoading: updating }] = useUpdateLegalHoldMutation();
  const [addNote, { isLoading: addingNote }] = useAddLegalHoldNoteMutation();
  const [releaseTarget, setReleaseTarget] = useState<LegalHold | null>(null);
  const [editTarget, setEditTarget] = useState<LegalHold | null>(null);
  const [notesId, setNotesId] = useState<string | null>(null);
  const [reviewAt, setReviewAt] = useState('');
  const [expiresAt, setExpiresAt] = useState('');
  const [userUuid, setUserUuid] = useState('');
  const [reason, setReason] = useState('');
  const [caseRef, setCaseRef] = useState('');

  const onPlace = async (e: FormEvent) => {
    e.preventDefault();
    if (!userUuid || !reason) return;
    try {
      await place({
        userUuid,
        reason,
        caseRef: caseRef || undefined,
        reviewAt: reviewAt ? reviewDayToInstant(reviewAt) : undefined,
        expiresAt: expiresAt ? expiryDayToInstant(expiresAt) : undefined
      }).unwrap();
      toast.success(t('adminCompliance.holds.placeSuccess'));
      setUserUuid('');
      setReason('');
      setCaseRef('');
      setReviewAt('');
      setExpiresAt('');
    } catch (err) {
      toast.error(
        erasureErrorMessage(err, t, t('adminCompliance.holds.placeError'))
      );
    }
  };
  const onRelease = async (releaseReason: string) => {
    if (!releaseTarget) return;
    try {
      await release({ id: releaseTarget.uuid, releaseReason }).unwrap();
      toast.success(t('adminCompliance.holds.releaseSuccess'));
      setReleaseTarget(null);
    } catch (err) {
      toast.error(
        erasureErrorMessage(err, t, t('adminCompliance.holds.releaseError'))
      );
    }
  };
  const onEdit = async (patch: LegalHoldPatch) => {
    if (!editTarget) return;
    try {
      await update({ id: editTarget.uuid, ...patch }).unwrap();
      toast.success(t('adminCompliance.holds.edit.success'));
      setEditTarget(null);
    } catch (err) {
      toast.error(
        erasureErrorMessage(err, t, t('adminCompliance.holds.edit.error'))
      );
    }
  };
  const onAddNote = async (text: string) => {
    if (!notesId) return false;
    try {
      await addNote({ id: notesId, text }).unwrap();
      return true;
    } catch (err) {
      toast.error(
        erasureErrorMessage(err, t, t('adminCompliance.holds.notes.error'))
      );
      return false;
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
        <>
          <SubtleBadge pill bg={original.active ? 'warning' : 'secondary'}>
            {original.active
              ? t('adminCompliance.status.active')
              : t('adminCompliance.status.released')}
          </SubtleBadge>
          {original.active && original.reviewDue && (
            <SubtleBadge pill bg="danger" className="ms-1">
              {t('adminCompliance.holds.reviewDue')}
            </SubtleBadge>
          )}
          {original.expired && (
            <SubtleBadge pill bg="secondary" className="ms-1">
              {t('adminCompliance.holds.expired')}
            </SubtleBadge>
          )}
        </>
      )
    },
    {
      id: 'dates',
      header: t('adminCompliance.holds.columns.dates'),
      enableSorting: false,
      meta: { headerProps: { className: 'text-900' } },
      cell: ({ row: { original } }: CellContext<LegalHold, unknown>) => (
        <div className="fs-11">
          <div>
            {t('adminCompliance.holds.reviewAt')}:{' '}
            {original.reviewAt ? formatDate(original.reviewAt) : '—'}
          </div>
          <div>
            {t('adminCompliance.holds.expiresAt')}:{' '}
            {original.expiresAt
              ? formatDate(expiryDisplayInstant(original.expiresAt))
              : '—'}
          </div>
        </div>
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
      cell: ({ row: { original } }: CellContext<LegalHold, unknown>) => (
        <div className="d-flex gap-1 justify-content-end">
          <IconButton
            size="sm"
            variant="outline-secondary"
            icon="comment"
            onClick={() => setNotesId(original.uuid)}
          >
            {t('adminCompliance.holds.notes.open', {
              count: original.notes?.length ?? 0
            })}
          </IconButton>
          {original.active && (
            <>
              {!original.expired && (
                <IconButton
                  size="sm"
                  variant="outline-secondary"
                  icon="edit"
                  onClick={() => setEditTarget(original)}
                >
                  {t('adminCompliance.holds.edit.open')}
                </IconButton>
              )}
              <IconButton
                size="sm"
                variant="outline-secondary"
                icon="unlock-alt"
                onClick={() => setReleaseTarget(original)}
              >
                {t('adminCompliance.holds.release')}
              </IconButton>
            </>
          )}
        </div>
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
              <Col md={3}>
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
              <Col md={3}>
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
                <Form.Label
                  className="fs-11 text-700 mb-1"
                  htmlFor="hold-place-review"
                >
                  {t('adminCompliance.holds.reviewAt')}
                </Form.Label>
                <Form.Control
                  id="hold-place-review"
                  size="sm"
                  type="date"
                  value={reviewAt}
                  onChange={e => setReviewAt(e.target.value)}
                />
              </Col>
              <Col md={2}>
                <Form.Label
                  className="fs-11 text-700 mb-1"
                  htmlFor="hold-place-expiry"
                >
                  {t('adminCompliance.holds.expiresAt')}
                </Form.Label>
                <Form.Control
                  id="hold-place-expiry"
                  size="sm"
                  type="date"
                  value={expiresAt}
                  onChange={e => setExpiresAt(e.target.value)}
                />
              </Col>
              <Col md={12}>
                <IconButton
                  size="sm"
                  type="submit"
                  variant="primary"
                  icon={faGavel}
                  className="text-nowrap"
                  disabled={isPlacing || !userUuid || !reason}
                >
                  {t('adminCompliance.holds.form.submit')}
                </IconButton>
              </Col>
            </Row>
          </Form>
        </Card.Body>
      </Card>

      <Form.Check
        type="switch"
        id="hold-show-released"
        className="mb-3 fs-10"
        label={t('adminCompliance.holds.showReleased')}
        checked={includeReleased}
        onChange={e => setIncludeReleased(e.target.checked)}
      />

      {isLoading ? (
        <Spinner animation="border" size="sm" className="mt-2" />
      ) : items.length === 0 ? (
        <EmptyState
          icon={faGavel}
          message={t('adminCompliance.holds.emptyMessage')}
          hint={t('adminCompliance.holds.emptyHint')}
        />
      ) : (
        <SearchablePagedTable
          data={items}
          columns={columns}
          searchPlaceholder={t('adminCompliance.holds.searchPlaceholder')}
        />
      )}

      <ReasonModal
        show={!!releaseTarget}
        title={t('adminCompliance.holds.releaseModal.title')}
        body={t('adminCompliance.holds.releaseModal.body')}
        confirmLabel={t('adminCompliance.holds.release')}
        busy={releasing}
        onCancel={() => setReleaseTarget(null)}
        onConfirm={onRelease}
      />
      <LegalHoldEditModal
        hold={editTarget}
        busy={updating}
        onCancel={() => setEditTarget(null)}
        onSave={onEdit}
      />
      <LegalHoldNotesModal
        hold={items.find(h => h.uuid === notesId) ?? null}
        busy={addingNote}
        onClose={() => setNotesId(null)}
        onAdd={onAddNote}
      />
    </>
  );
};

export default LegalHoldsTab;
