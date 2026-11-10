import { useEffect, useState } from 'react';
import { Button, Form, Modal } from 'react-bootstrap';
import { useTranslation } from 'react-i18next';
import type { LegalHold, UpdateLegalHoldArgs } from 'store/api/complianceApi';
import {
  expiryDayToInstant,
  instantToExpiryDay,
  instantToReviewDay,
  reviewDayToInstant
} from './legalHoldDates';

export type LegalHoldPatch = Omit<UpdateLegalHoldArgs, 'id'>;

interface Props {
  hold: LegalHold | null;
  busy: boolean;
  onCancel: () => void;
  onSave: (patch: LegalHoldPatch) => void;
}

// LegalHoldEditModal changes a hold's review and expiry dates. A date can be
// cleared (the hold then has none), which is sent as an explicit clear flag.
const LegalHoldEditModal = ({ hold, busy, onCancel, onSave }: Props) => {
  const { t } = useTranslation();
  const [reviewAt, setReviewAt] = useState('');
  const [expiresAt, setExpiresAt] = useState('');
  useEffect(() => {
    setReviewAt(instantToReviewDay(hold?.reviewAt));
    setExpiresAt(instantToExpiryDay(hold?.expiresAt));
  }, [hold]);

  // An emptied field that had a value is a clear; an unchanged one is omitted.
  const buildPatch = (): LegalHoldPatch => {
    const patch: LegalHoldPatch = {};
    const wasReview = instantToReviewDay(hold?.reviewAt);
    const wasExpiry = instantToExpiryDay(hold?.expiresAt);
    if (!reviewAt && wasReview) patch.clearReviewAt = true;
    else if (reviewAt && reviewAt !== wasReview)
      patch.reviewAt = reviewDayToInstant(reviewAt);
    if (!expiresAt && wasExpiry) patch.clearExpiresAt = true;
    else if (expiresAt && expiresAt !== wasExpiry)
      patch.expiresAt = expiryDayToInstant(expiresAt);
    return patch;
  };
  const patch = buildPatch();
  const empty = Object.keys(patch).length === 0;
  const submit = () => onSave(patch);

  return (
    <Modal show={!!hold} onHide={onCancel} centered>
      <Modal.Header closeButton>
        <Modal.Title as="h5">
          {t('adminCompliance.holds.edit.title')}
        </Modal.Title>
      </Modal.Header>
      <Modal.Body>
        <Form.Group className="mb-3" controlId="hold-edit-review">
          <Form.Label>{t('adminCompliance.holds.edit.reviewAt')}</Form.Label>
          <Form.Control
            type="date"
            value={reviewAt}
            onChange={e => setReviewAt(e.target.value)}
          />
          {reviewAt && (
            <Button
              variant="link"
              size="sm"
              className="p-0 mt-1"
              onClick={() => setReviewAt('')}
            >
              {t('adminCompliance.holds.edit.clearReviewAt')}
            </Button>
          )}
        </Form.Group>
        <Form.Group controlId="hold-edit-expiry">
          <Form.Label>{t('adminCompliance.holds.edit.expiresAt')}</Form.Label>
          <Form.Control
            type="date"
            value={expiresAt}
            onChange={e => setExpiresAt(e.target.value)}
          />
          {expiresAt && (
            <Button
              variant="link"
              size="sm"
              className="p-0 mt-1"
              onClick={() => setExpiresAt('')}
            >
              {t('adminCompliance.holds.edit.clearExpiresAt')}
            </Button>
          )}
          <Form.Text muted>
            {t('adminCompliance.holds.edit.expiryHint')}
          </Form.Text>
        </Form.Group>
      </Modal.Body>
      <Modal.Footer>
        <Button variant="orkestra-default" onClick={onCancel} disabled={busy}>
          {t('adminCompliance.saveModal.cancel')}
        </Button>
        <Button
          variant="orkestra-primary"
          onClick={submit}
          disabled={busy || empty}
        >
          {t('adminCompliance.holds.edit.submit')}
        </Button>
      </Modal.Footer>
    </Modal>
  );
};

export default LegalHoldEditModal;
