// Side-by-side resolver for one marketing_conflict_reviews row.
//
// Layout:
//   ┌──────────────────────────────────────────────┐
//   │ Existing record  │  Incoming record           │
//   │ (snapshot at     │  (importer payload)        │
//   │  park time)      │                            │
//   ├──────────────────────────────────────────────┤
//   │ Per-conflict row:                            │
//   │   field: existing vs incoming                │
//   │   ○ Keep existing   ○ Take incoming          │
//   └──────────────────────────────────────────────┘
//
// Action buttons:
//   - Keep existing   → POST /resolve {action: keep_existing}
//   - Take incoming   → POST /resolve {action: take_incoming}
//   - Apply manual    → POST /resolve {action: manual_merge, fieldOverrides}
//   - Dismiss         → POST /dismiss
//
// Resolved/dismissed reviews open in read-only mode (no buttons).

import { useMemo, useState } from 'react';
import { Modal, Button, Table, Badge, Form } from 'react-bootstrap';
import { useTranslation } from 'react-i18next';
import { toast } from 'react-toastify';
import {
  useResolveConflictReviewMutation,
  useDismissConflictReviewMutation
} from 'store/api/marketingApi';
import type { ConflictReview } from 'types/marketing';

type ResolverChoice = 'keep' | 'take';

interface Props {
  review: ConflictReview;
  onClose: () => void;
}

const formatValue = (v: unknown): string => {
  if (v === null || v === undefined || v === '') return '—';
  if (typeof v === 'string') return v;
  if (typeof v === 'number' || typeof v === 'boolean') return String(v);
  try {
    return JSON.stringify(v);
  } catch {
    return String(v);
  }
};

const ReviewResolverModal: React.FC<Props> = ({ review, onClose }) => {
  const { t } = useTranslation();
  const readOnly = review.status !== 'pending';

  // Per-conflict choice state. Defaults to "keep" so a no-op submit
  // is conservative (the existing record wins by default).
  const [choices, setChoices] = useState<Record<string, ResolverChoice>>(() =>
    Object.fromEntries(
      review.conflicts.map(c => [c.field, 'keep' as ResolverChoice])
    )
  );
  const [notes, setNotes] = useState('');

  const [resolve, { isLoading: resolving }] =
    useResolveConflictReviewMutation();
  const [dismiss, { isLoading: dismissing }] =
    useDismissConflictReviewMutation();

  const submitting = resolving || dismissing;

  // Field overrides for manual_merge — only includes fields where
  // the operator picked "take incoming". Keep-existing fields are
  // omitted so they retain their current value on the existing record.
  const manualOverrides = useMemo(() => {
    const out: Record<string, unknown> = {};
    for (const c of review.conflicts) {
      if (choices[c.field] === 'take') {
        out[c.field] = c.incomingValue;
      }
    }
    return out;
  }, [choices, review.conflicts]);

  const allKeepExisting = Object.values(choices).every(v => v === 'keep');
  const allTakeIncoming = Object.values(choices).every(v => v === 'take');

  const callResolve = async (
    action: 'keep_existing' | 'take_incoming' | 'manual_merge'
  ) => {
    try {
      await resolve({
        id: review.uuid,
        importJobUuid: review.importJobUuid,
        body: {
          action,
          fieldOverrides:
            action === 'manual_merge' ? manualOverrides : undefined,
          notes: notes || undefined
        }
      }).unwrap();
      toast.success(t('marketing.reviews.modal.toast.resolved'));
      onClose();
    } catch (e) {
      toast.error(
        t('marketing.reviews.modal.toast.resolveFailed', {
          message: e instanceof Error ? e.message : ''
        })
      );
    }
  };

  const callDismiss = async () => {
    try {
      await dismiss({
        id: review.uuid,
        importJobUuid: review.importJobUuid,
        body: { notes: notes || undefined }
      }).unwrap();
      toast.success(t('marketing.reviews.modal.toast.dismissed'));
      onClose();
    } catch (e) {
      toast.error(
        t('marketing.reviews.modal.toast.dismissFailed', {
          message: e instanceof Error ? e.message : ''
        })
      );
    }
  };

  return (
    <Modal show onHide={onClose} size="lg" backdrop="static">
      <Modal.Header closeButton>
        <Modal.Title>
          {t('marketing.reviews.modal.title')}
          {' — '}
          <Badge bg="light" text="dark">
            {review.targetKind}
          </Badge>
        </Modal.Title>
      </Modal.Header>
      <Modal.Body>
        <div className="mb-3">
          <small className="text-muted">
            {t('marketing.reviews.modal.importJob')}{' '}
            <code>{review.importJobUuid}</code> ·{' '}
            {t('marketing.reviews.modal.existingUuid')}{' '}
            <code>{review.existingUuid}</code>
          </small>
        </div>

        <h6 className="mb-2">{t('marketing.reviews.modal.conflicts')}</h6>
        <Table size="sm" bordered>
          <thead className="bg-200">
            <tr>
              <th style={{ width: '20%' }}>
                {t('marketing.reviews.modal.field')}
              </th>
              <th>{t('marketing.reviews.modal.existing')}</th>
              <th>{t('marketing.reviews.modal.incoming')}</th>
              <th style={{ width: '160px' }}>
                {t('marketing.reviews.modal.choose')}
              </th>
            </tr>
          </thead>
          <tbody>
            {review.conflicts.map(c => (
              <tr key={c.field}>
                <td>
                  <code className="fs-10">{c.field}</code>
                  {c.severity === 'soft' && (
                    <div>
                      <Badge bg="info" className="fs-10">
                        {t('marketing.reviews.modal.softMatch')}
                      </Badge>
                    </div>
                  )}
                </td>
                <td>
                  <small>{formatValue(c.existingValue)}</small>
                </td>
                <td>
                  <small>{formatValue(c.incomingValue)}</small>
                </td>
                <td>
                  <Form.Check
                    type="radio"
                    inline
                    name={`choice-${c.field}`}
                    id={`keep-${c.field}`}
                    label={t('marketing.reviews.modal.keep')}
                    checked={choices[c.field] === 'keep'}
                    disabled={readOnly}
                    onChange={() =>
                      setChoices(prev => ({ ...prev, [c.field]: 'keep' }))
                    }
                  />
                  <Form.Check
                    type="radio"
                    inline
                    name={`choice-${c.field}`}
                    id={`take-${c.field}`}
                    label={t('marketing.reviews.modal.take')}
                    checked={choices[c.field] === 'take'}
                    disabled={readOnly}
                    onChange={() =>
                      setChoices(prev => ({ ...prev, [c.field]: 'take' }))
                    }
                  />
                </td>
              </tr>
            ))}
          </tbody>
        </Table>

        {!readOnly && (
          <Form.Group className="mt-3">
            <Form.Label className="fs-10">
              {t('marketing.reviews.modal.notes')}
            </Form.Label>
            <Form.Control
              as="textarea"
              rows={2}
              value={notes}
              onChange={e => setNotes(e.target.value)}
              placeholder={t('marketing.reviews.modal.notesPlaceholder')}
            />
          </Form.Group>
        )}

        {readOnly && review.resolution && (
          <div className="mt-3 text-muted fs-10">
            <strong>{t('marketing.reviews.modal.resolvedWith')}: </strong>
            <code>{review.resolution.action}</code>
            {review.resolvedBy && (
              <>
                {' '}
                {t('marketing.reviews.modal.by')}{' '}
                <code>{review.resolvedBy}</code>
              </>
            )}
            {review.resolvedNotes && (
              <div className="mt-1">
                <em>{review.resolvedNotes}</em>
              </div>
            )}
          </div>
        )}
      </Modal.Body>
      {!readOnly && (
        <Modal.Footer className="d-flex justify-content-between">
          <Button
            variant="outline-danger"
            size="sm"
            onClick={callDismiss}
            disabled={submitting}
          >
            {t('marketing.reviews.modal.dismiss')}
          </Button>
          <div className="d-flex gap-2">
            <Button
              variant="outline-secondary"
              size="sm"
              onClick={() => callResolve('keep_existing')}
              disabled={submitting}
            >
              {t('marketing.reviews.modal.keepAll')}
            </Button>
            <Button
              variant="outline-primary"
              size="sm"
              onClick={() => callResolve('take_incoming')}
              disabled={submitting}
            >
              {t('marketing.reviews.modal.takeAll')}
            </Button>
            <Button
              variant="primary"
              size="sm"
              onClick={() => callResolve('manual_merge')}
              disabled={submitting || allKeepExisting || allTakeIncoming}
              title={
                allKeepExisting
                  ? t('marketing.reviews.modal.useKeepAllHint')
                  : allTakeIncoming
                    ? t('marketing.reviews.modal.useTakeAllHint')
                    : undefined
              }
            >
              {t('marketing.reviews.modal.applyManual')}
            </Button>
          </div>
        </Modal.Footer>
      )}
    </Modal>
  );
};

export default ReviewResolverModal;
