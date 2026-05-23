// Suspend / Reinstate confirmation modal.
//
// The two flows are paired in one component because they share the
// modal chrome + the same RTK Query invalidation pattern; the only
// difference is whether the reason textarea renders + which mutation
// fires.

import { useState } from 'react';
import { Alert, Button, Form, Modal } from 'react-bootstrap';
import { useTranslation } from 'react-i18next';
import {
  useReinstateCardMutation,
  useSuspendCardMutation
} from 'store/api/marketingApi';
import type { Card as MarketingCard } from 'types/marketing';

interface SuspendCardModalProps {
  mode: 'suspend' | 'reinstate';
  card: MarketingCard;
  onClose: () => void;
}

const SuspendCardModal: React.FC<SuspendCardModalProps> = ({
  mode,
  card,
  onClose
}) => {
  const { t } = useTranslation();
  const [reason, setReason] = useState('');
  const [suspendCard, suspendState] = useSuspendCardMutation();
  const [reinstateCard, reinstateState] = useReinstateCardMutation();
  const isLoading = suspendState.isLoading || reinstateState.isLoading;
  const error = suspendState.error || reinstateState.error;

  const titleKey =
    mode === 'suspend'
      ? 'marketing.cards.suspend.title'
      : 'marketing.cards.reinstate.title';
  const bodyKey =
    mode === 'suspend'
      ? 'marketing.cards.suspend.body'
      : 'marketing.cards.reinstate.body';
  const submitKey =
    mode === 'suspend'
      ? 'marketing.cards.suspend.submit'
      : 'marketing.cards.reinstate.submit';

  const onSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    try {
      if (mode === 'suspend') {
        await suspendCard({
          id: card.uuid,
          body: { reason },
          personUuid: card.personUuid
        }).unwrap();
      } else {
        await reinstateCard({
          id: card.uuid,
          personUuid: card.personUuid
        }).unwrap();
      }
      onClose();
    } catch {
      /* error alert handles display */
    }
  };

  return (
    <Modal show onHide={onClose} centered>
      <Form onSubmit={onSubmit}>
        <Modal.Header closeButton>
          <Modal.Title>{t(titleKey, { code: card.code })}</Modal.Title>
        </Modal.Header>
        <Modal.Body>
          {error && (
            <Alert variant="danger">{t('marketing.cards.actionError')}</Alert>
          )}
          <p className="text-muted">{t(bodyKey, { code: card.code })}</p>
          {mode === 'suspend' && (
            <Form.Group>
              <Form.Label>
                {t('marketing.cards.suspend.reasonLabel')}
              </Form.Label>
              <Form.Control
                as="textarea"
                rows={3}
                value={reason}
                onChange={e => setReason(e.target.value)}
                required
              />
            </Form.Group>
          )}
        </Modal.Body>
        <Modal.Footer>
          <Button variant="secondary" type="button" onClick={onClose}>
            {t('marketing.cards.cancel')}
          </Button>
          <Button
            type="submit"
            variant={mode === 'suspend' ? 'warning' : 'primary'}
            disabled={isLoading || (mode === 'suspend' && !reason.trim())}
          >
            {isLoading ? t('marketing.cards.submitting') : t(submitKey)}
          </Button>
        </Modal.Footer>
      </Form>
    </Modal>
  );
};

export default SuspendCardModal;
