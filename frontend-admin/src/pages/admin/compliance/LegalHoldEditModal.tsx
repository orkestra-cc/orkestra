import { useEffect, useState } from 'react';
import { Button, Form, Modal } from 'react-bootstrap';
import { useTranslation } from 'react-i18next';
import type { LegalHold, UpdateLegalHoldArgs } from 'store/api/complianceApi';

export type LegalHoldPatch = Omit<UpdateLegalHoldArgs, 'id'>;

interface Props {
  hold: LegalHold | null;
  busy: boolean;
  onCancel: () => void;
  onSave: (patch: LegalHoldPatch) => void;
}

const day = (iso?: string) => (iso ? iso.slice(0, 10) : '');
// Date inputs carry a calendar day; the backend wants an instant.
export const dayToInstant = (d: string) => `${d}T00:00:00Z`;

// LegalHoldEditModal changes a hold's review and expiry dates. A date can be
// cleared (the hold then has none), which is sent as an explicit clear flag.
const LegalHoldEditModal = ({ hold, busy, onCancel, onSave }: Props) => {
  const { t } = useTranslation();
  const [reviewAt, setReviewAt] = useState('');
  const [expiresAt, setExpiresAt] = useState('');
  const [clearReview, setClearReview] = useState(false);
  const [clearExpiry, setClearExpiry] = useState(false);
  useEffect(() => {
    setReviewAt(day(hold?.reviewAt));
    setExpiresAt(day(hold?.expiresAt));
    setClearReview(false);
    setClearExpiry(false);
  }, [hold]);

  const submit = () => {
    const patch: LegalHoldPatch = {};
    if (clearReview) patch.clearReviewAt = true;
    else if (reviewAt && reviewAt !== day(hold?.reviewAt))
      patch.reviewAt = dayToInstant(reviewAt);
    if (clearExpiry) patch.clearExpiresAt = true;
    else if (expiresAt && expiresAt !== day(hold?.expiresAt))
      patch.expiresAt = dayToInstant(expiresAt);
    onSave(patch);
  };

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
            disabled={clearReview}
            onChange={e => setReviewAt(e.target.value)}
          />
          {hold?.reviewAt && (
            <Form.Check
              className="mt-1"
              id="hold-edit-clear-review"
              label={t('adminCompliance.holds.edit.clearReviewAt')}
              checked={clearReview}
              onChange={e => setClearReview(e.target.checked)}
            />
          )}
        </Form.Group>
        <Form.Group controlId="hold-edit-expiry">
          <Form.Label>{t('adminCompliance.holds.edit.expiresAt')}</Form.Label>
          <Form.Control
            type="date"
            value={expiresAt}
            disabled={clearExpiry}
            onChange={e => setExpiresAt(e.target.value)}
          />
          {hold?.expiresAt && (
            <Form.Check
              className="mt-1"
              id="hold-edit-clear-expiry"
              label={t('adminCompliance.holds.edit.clearExpiresAt')}
              checked={clearExpiry}
              onChange={e => setClearExpiry(e.target.checked)}
            />
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
        <Button variant="orkestra-primary" onClick={submit} disabled={busy}>
          {t('adminCompliance.holds.edit.submit')}
        </Button>
      </Modal.Footer>
    </Modal>
  );
};

export default LegalHoldEditModal;
