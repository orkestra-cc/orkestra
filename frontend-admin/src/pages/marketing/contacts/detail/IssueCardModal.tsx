// Issue-card modal — pick type + tier + benefits + optional expiresAt.
// Backed by useIssueCardMutation; the slice invalidates the cards tag
// for this person so the table refreshes after success.

import { useEffect, useMemo, useState } from 'react';
import { Alert, Button, Form, Modal } from 'react-bootstrap';
import { useTranslation } from 'react-i18next';
import {
  useIssueCardMutation,
  useListCardTypesQuery
} from 'store/api/marketingApi';
import type { IssueCardPayload } from 'types/marketing';

interface IssueCardModalProps {
  personId: string;
  onClose: () => void;
}

const IssueCardModal: React.FC<IssueCardModalProps> = ({
  personId,
  onClose
}) => {
  const { t } = useTranslation();
  const { data: cardTypes } = useListCardTypesQuery({ activeOnly: true });
  const [issueCard, { isLoading, error }] = useIssueCardMutation();

  const [cardTypeUuid, setCardTypeUuid] = useState('');
  const [tier, setTier] = useState('');
  const [benefitsCsv, setBenefitsCsv] = useState('');
  const [expiresAt, setExpiresAt] = useState('');
  const [notes, setNotes] = useState('');

  const selectedType = useMemo(
    () => cardTypes?.items?.find(c => c.uuid === cardTypeUuid),
    [cardTypes, cardTypeUuid]
  );

  // Auto-populate the benefits textarea from the selected type's
  // defaults so the operator only edits when they need to override.
  useEffect(() => {
    if (selectedType) {
      setBenefitsCsv((selectedType.defaultBenefits ?? []).join(', '));
      // Reset tier when type changes — the prior tier may not be valid
      // for the new type's tiers list.
      setTier('');
    }
  }, [selectedType]);

  const onSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    const payload: IssueCardPayload = {
      cardTypeUuid,
      tier: tier || undefined,
      benefits: benefitsCsv
        ? benefitsCsv
            .split(',')
            .map(s => s.trim())
            .filter(Boolean)
        : undefined,
      // The backend accepts RFC3339; the <input type="date"> emits
      // YYYY-MM-DD, so append midnight UTC.
      expiresAt: expiresAt ? `${expiresAt}T00:00:00Z` : undefined,
      notes: notes || undefined
    };
    try {
      await issueCard({ personId, body: payload }).unwrap();
      onClose();
    } catch {
      // Inline alert renders RTK Query error
    }
  };

  return (
    <Modal show onHide={onClose} centered>
      <Form onSubmit={onSubmit}>
        <Modal.Header closeButton>
          <Modal.Title>{t('marketing.cards.issue.title')}</Modal.Title>
        </Modal.Header>
        <Modal.Body>
          {error && (
            <Alert variant="danger">{t('marketing.cards.issue.error')}</Alert>
          )}
          <Form.Group className="mb-3">
            <Form.Label>{t('marketing.cards.issue.typeLabel')}</Form.Label>
            <Form.Select
              value={cardTypeUuid}
              onChange={e => setCardTypeUuid(e.target.value)}
              required
            >
              <option value="">
                {t('marketing.cards.issue.typePlaceholder')}
              </option>
              {cardTypes?.items?.map(c => (
                <option key={c.uuid} value={c.uuid}>
                  {c.displayName}
                </option>
              ))}
            </Form.Select>
          </Form.Group>
          {selectedType && (selectedType.tiers ?? []).length > 0 && (
            <Form.Group className="mb-3">
              <Form.Label>{t('marketing.cards.issue.tierLabel')}</Form.Label>
              <Form.Select value={tier} onChange={e => setTier(e.target.value)}>
                <option value="">
                  {t('marketing.cards.issue.tierPlaceholder')}
                </option>
                {selectedType.tiers!.map(tt => (
                  <option key={tt} value={tt}>
                    {tt}
                  </option>
                ))}
              </Form.Select>
            </Form.Group>
          )}
          <Form.Group className="mb-3">
            <Form.Label>{t('marketing.cards.issue.benefitsLabel')}</Form.Label>
            <Form.Control
              as="textarea"
              rows={2}
              value={benefitsCsv}
              onChange={e => setBenefitsCsv(e.target.value)}
            />
            <Form.Text className="text-muted">
              {t('marketing.cards.issue.benefitsHelp')}
            </Form.Text>
          </Form.Group>
          <Form.Group className="mb-3">
            <Form.Label>{t('marketing.cards.issue.expiresLabel')}</Form.Label>
            <Form.Control
              type="date"
              value={expiresAt}
              onChange={e => setExpiresAt(e.target.value)}
            />
          </Form.Group>
          <Form.Group className="mb-0">
            <Form.Label>{t('marketing.cards.issue.notesLabel')}</Form.Label>
            <Form.Control
              as="textarea"
              rows={2}
              value={notes}
              onChange={e => setNotes(e.target.value)}
            />
          </Form.Group>
        </Modal.Body>
        <Modal.Footer>
          <Button variant="secondary" onClick={onClose} type="button">
            {t('marketing.cards.issue.cancel')}
          </Button>
          <Button
            type="submit"
            variant="primary"
            disabled={isLoading || !cardTypeUuid}
          >
            {isLoading
              ? t('marketing.cards.issue.submitting')
              : t('marketing.cards.issue.submit')}
          </Button>
        </Modal.Footer>
      </Form>
    </Modal>
  );
};

export default IssueCardModal;
