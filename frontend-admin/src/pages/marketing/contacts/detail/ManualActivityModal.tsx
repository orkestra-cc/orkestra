// ManualActivityModal — POST /v1/marketing/activities form.
// Restricted to the four ManualKinds the backend accepts; the kind
// dropdown enforces the contract at the UI surface so a 400 from the
// server is a fallback, not the primary signal.
//
// Notes/payload field is a plain textarea — operators type free-form
// context. The handler stores it as `payload.note` for call/meeting/
// note kinds; corrected_by goes through the dedicated /correct route
// (not this modal).

import { useState } from 'react';
import { Alert, Button, Form, Modal } from 'react-bootstrap';
import { useTranslation } from 'react-i18next';
import { useCreateActivityMutation } from 'store/api/marketingApi';
import type { ActivityKind } from 'types/marketing';

// CALL_MEETING_NOTE excludes corrected_by — that flow has its own UI
// affordance (a Correct button in the timeline row, deferred to a
// Phase 2 follow-up PR).
const MODAL_KINDS: ActivityKind[] = ['call_made', 'meeting_held', 'note_added'];

interface ManualActivityModalProps {
  personId: string;
  show: boolean;
  onHide: () => void;
}

const ManualActivityModal: React.FC<ManualActivityModalProps> = ({
  personId,
  show,
  onHide
}) => {
  const { t } = useTranslation();
  const [kind, setKind] = useState<ActivityKind>('note_added');
  const [occurredAt, setOccurredAt] = useState('');
  const [note, setNote] = useState('');
  const [createActivity, { isLoading, error, reset }] =
    useCreateActivityMutation();

  const handleClose = () => {
    setNote('');
    setOccurredAt('');
    setKind('note_added');
    reset();
    onHide();
  };

  const onSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    try {
      await createActivity({
        personUuid: personId,
        kind,
        occurredAt: occurredAt ? new Date(occurredAt).toISOString() : undefined,
        payload: note ? { note } : undefined
      }).unwrap();
      handleClose();
    } catch {
      // Error surfaces via the `error` selector; modal stays open.
    }
  };

  return (
    <Modal show={show} onHide={handleClose} centered>
      <Form onSubmit={onSubmit}>
        <Modal.Header closeButton>
          <Modal.Title>{t('marketing.timeline.modal.title')}</Modal.Title>
        </Modal.Header>
        <Modal.Body>
          {error && (
            <Alert variant="danger">
              {t('marketing.timeline.modal.submitError')}
            </Alert>
          )}
          <Form.Group className="mb-3">
            <Form.Label>{t('marketing.timeline.modal.kindLabel')}</Form.Label>
            <Form.Select
              value={kind}
              onChange={e => setKind(e.target.value as ActivityKind)}
            >
              {MODAL_KINDS.map(k => (
                <option key={k} value={k}>
                  {t(`marketing.timeline.kinds.${k}`)}
                </option>
              ))}
            </Form.Select>
          </Form.Group>
          <Form.Group className="mb-3">
            <Form.Label>
              {t('marketing.timeline.modal.occurredAtLabel')}
            </Form.Label>
            <Form.Control
              type="datetime-local"
              value={occurredAt}
              onChange={e => setOccurredAt(e.target.value)}
            />
            <Form.Text className="text-muted">
              {t('marketing.timeline.modal.occurredAtHelp')}
            </Form.Text>
          </Form.Group>
          <Form.Group>
            <Form.Label>{t('marketing.timeline.modal.noteLabel')}</Form.Label>
            <Form.Control
              as="textarea"
              rows={4}
              value={note}
              onChange={e => setNote(e.target.value)}
              placeholder={t('marketing.timeline.modal.notePlaceholder') || ''}
            />
          </Form.Group>
        </Modal.Body>
        <Modal.Footer>
          <Button
            variant="secondary"
            onClick={handleClose}
            disabled={isLoading}
          >
            {t('marketing.timeline.modal.cancel')}
          </Button>
          <Button type="submit" variant="primary" disabled={isLoading}>
            {isLoading
              ? t('marketing.timeline.modal.saving')
              : t('marketing.timeline.modal.save')}
          </Button>
        </Modal.Footer>
      </Form>
    </Modal>
  );
};

export default ManualActivityModal;
