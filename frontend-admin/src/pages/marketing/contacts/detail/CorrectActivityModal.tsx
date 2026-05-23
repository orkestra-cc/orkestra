// Correct-activity modal — operator types a reason, the backend
// inserts a corrected_by row pointing at the original via
// refs.correctsActivityUuid. The next eager + nightly recompute
// de-applies the original from the snapshot.

import { useState } from 'react';
import { Alert, Button, Form, Modal } from 'react-bootstrap';
import { useTranslation } from 'react-i18next';
import { useCorrectActivityMutation } from 'store/api/marketingApi';
import type { Activity } from 'types/marketing';

interface CorrectActivityModalProps {
  activity: Activity;
  onClose: () => void;
}

const CorrectActivityModal: React.FC<CorrectActivityModalProps> = ({
  activity,
  onClose
}) => {
  const { t } = useTranslation();
  const [reason, setReason] = useState('');
  const [correctActivity, { isLoading, error }] = useCorrectActivityMutation();

  const onSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!reason.trim()) return;
    try {
      await correctActivity({
        id: activity.uuid,
        reason: reason.trim(),
        personUuid: activity.personUuid
      }).unwrap();
      onClose();
    } catch {
      /* alert handles */
    }
  };

  return (
    <Modal show onHide={onClose} centered>
      <Form onSubmit={onSubmit}>
        <Modal.Header closeButton>
          <Modal.Title>{t('marketing.corrections.modal.title')}</Modal.Title>
        </Modal.Header>
        <Modal.Body>
          {error && (
            <Alert variant="danger">
              {t('marketing.corrections.modal.error')}
            </Alert>
          )}
          <p className="text-muted">
            {t('marketing.corrections.modal.body', {
              kind: activity.kind,
              when: new Date(activity.occurredAt).toLocaleString()
            })}
          </p>
          <Form.Group>
            <Form.Label>
              {t('marketing.corrections.modal.reasonLabel')}
            </Form.Label>
            <Form.Control
              as="textarea"
              rows={4}
              value={reason}
              onChange={e => setReason(e.target.value)}
              required
            />
            <Form.Text className="text-muted">
              {t('marketing.corrections.modal.reasonHelp')}
            </Form.Text>
          </Form.Group>
        </Modal.Body>
        <Modal.Footer>
          <Button variant="secondary" type="button" onClick={onClose}>
            {t('marketing.corrections.modal.cancel')}
          </Button>
          <Button
            type="submit"
            variant="warning"
            disabled={isLoading || !reason.trim()}
          >
            {isLoading
              ? t('marketing.corrections.modal.submitting')
              : t('marketing.corrections.modal.submit')}
          </Button>
        </Modal.Footer>
      </Form>
    </Modal>
  );
};

export default CorrectActivityModal;
