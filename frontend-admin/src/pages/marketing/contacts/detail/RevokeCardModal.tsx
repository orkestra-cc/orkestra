// Revoke-card modal. Friction guard: the operator must retype the
// card's code before the submit button enables. Matches the
// score-profile delete modal's irreversibility safeguard.

import { useState } from 'react';
import { Alert, Button, Form, Modal } from 'react-bootstrap';
import { useTranslation } from 'react-i18next';
import { useRevokeCardMutation } from 'store/api/marketingApi';
import type { Card as MarketingCard } from 'types/marketing';

interface RevokeCardModalProps {
  card: MarketingCard;
  onClose: () => void;
}

const RevokeCardModal: React.FC<RevokeCardModalProps> = ({ card, onClose }) => {
  const { t } = useTranslation();
  const [reason, setReason] = useState('');
  const [confirmCode, setConfirmCode] = useState('');
  const [revokeCard, { isLoading, error }] = useRevokeCardMutation();

  const codeMatches = confirmCode.trim() === card.code;

  const onSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!codeMatches) return;
    try {
      await revokeCard({
        id: card.uuid,
        body: { reason },
        personUuid: card.personUuid
      }).unwrap();
      onClose();
    } catch {
      /* error alert */
    }
  };

  return (
    <Modal show onHide={onClose} centered>
      <Form onSubmit={onSubmit}>
        <Modal.Header closeButton>
          <Modal.Title>
            {t('marketing.cards.revoke.title', { code: card.code })}
          </Modal.Title>
        </Modal.Header>
        <Modal.Body>
          {error && (
            <Alert variant="danger">{t('marketing.cards.actionError')}</Alert>
          )}
          <Alert variant="warning" className="mb-3">
            {t('marketing.cards.revoke.warning')}
          </Alert>
          <Form.Group className="mb-3">
            <Form.Label>{t('marketing.cards.revoke.reasonLabel')}</Form.Label>
            <Form.Control
              as="textarea"
              rows={3}
              value={reason}
              onChange={e => setReason(e.target.value)}
              required
            />
          </Form.Group>
          <Form.Group>
            <Form.Label>
              {t('marketing.cards.revoke.confirmCodeLabel', {
                code: card.code
              })}
            </Form.Label>
            <Form.Control
              type="text"
              value={confirmCode}
              onChange={e => setConfirmCode(e.target.value)}
              className="font-monospace"
              autoComplete="off"
              required
            />
          </Form.Group>
        </Modal.Body>
        <Modal.Footer>
          <Button variant="secondary" type="button" onClick={onClose}>
            {t('marketing.cards.cancel')}
          </Button>
          <Button
            type="submit"
            variant="danger"
            disabled={isLoading || !codeMatches || !reason.trim()}
          >
            {isLoading
              ? t('marketing.cards.submitting')
              : t('marketing.cards.revoke.submit')}
          </Button>
        </Modal.Footer>
      </Form>
    </Modal>
  );
};

export default RevokeCardModal;
