// Phase 4 — Marketing Cards tab on the contact-detail page.
//
// Lists every card issued to the person + exposes the four lifecycle
// mutations (Issue / Suspend / Reinstate / Revoke). Three modal
// components live in the same directory:
//   IssueCardModal       — pick card type + tier + benefits + expiresAt
//   SuspendCardModal     — capture reason; also reused for reinstate
//                          confirmation (no reason field on reinstate)
//   RevokeCardModal      — irreversible-action friction: operator must
//                          retype the card code before submit
//
// Server-side gating drives whether each action button is rendered —
// the operator's Cedar role is not available client-side, so we lean
// on the API surface returning 403 if the operator lacks the bucket.
// The action buttons stay visible; failures show a toast.

import { useMemo, useState } from 'react';
import { Badge, Button, Dropdown, Table } from 'react-bootstrap';
import { useTranslation } from 'react-i18next';
import {
  useListCardTypesQuery,
  useListPersonCardsQuery
} from 'store/api/marketingApi';
import type { Card as MarketingCard, CardStatus } from 'types/marketing';
import IssueCardModal from './IssueCardModal';
import SuspendCardModal from './SuspendCardModal';
import RevokeCardModal from './RevokeCardModal';

interface CardsTabProps {
  personId: string;
}

const statusVariant: Record<CardStatus, string> = {
  active: 'success',
  suspended: 'warning',
  revoked: 'secondary'
};

const CardsTab: React.FC<CardsTabProps> = ({ personId }) => {
  const { t } = useTranslation();
  const { data, isLoading } = useListPersonCardsQuery(personId);
  const { data: cardTypes } = useListCardTypesQuery(undefined);

  const cardTypesByUuid = useMemo(
    () => Object.fromEntries((cardTypes?.items ?? []).map(c => [c.uuid, c])),
    [cardTypes]
  );

  const [issueOpen, setIssueOpen] = useState(false);
  const [suspendTarget, setSuspendTarget] = useState<MarketingCard | null>(
    null
  );
  const [reinstateTarget, setReinstateTarget] = useState<MarketingCard | null>(
    null
  );
  const [revokeTarget, setRevokeTarget] = useState<MarketingCard | null>(null);

  const dash = t('marketing.contacts.detail.dash');

  return (
    <>
      <div className="d-flex justify-content-end mb-3">
        <Button size="sm" variant="primary" onClick={() => setIssueOpen(true)}>
          {t('marketing.cards.actions.issue')}
        </Button>
      </div>

      {isLoading && (
        <p className="text-muted mb-0">{t('marketing.cards.loading')}</p>
      )}

      {!isLoading && !data?.items?.length && (
        <p className="text-muted mb-0">{t('marketing.cards.empty')}</p>
      )}

      {data?.items && data.items.length > 0 && (
        <Table size="sm" responsive>
          <thead>
            <tr>
              <th>{t('marketing.cards.col.code')}</th>
              <th>{t('marketing.cards.col.type')}</th>
              <th>{t('marketing.cards.col.tier')}</th>
              <th>{t('marketing.cards.col.status')}</th>
              <th>{t('marketing.cards.col.issuedAt')}</th>
              <th>{t('marketing.cards.col.expiresAt')}</th>
              <th style={{ width: 60 }}></th>
            </tr>
          </thead>
          <tbody>
            {data.items.map(card => {
              const type = cardTypesByUuid[card.cardTypeUuid];
              return (
                <tr key={card.uuid}>
                  <td>
                    <code>{card.code}</code>
                  </td>
                  <td>
                    {type?.displayName ?? (
                      <code className="text-muted">
                        {card.cardTypeUuid.slice(0, 8)}
                      </code>
                    )}
                  </td>
                  <td>{card.tier || dash}</td>
                  <td>
                    <Badge bg={statusVariant[card.status]} pill>
                      {t(`marketing.cards.status.${card.status}`)}
                    </Badge>
                  </td>
                  <td>
                    <small className="text-muted">
                      {new Date(card.issuedAt).toLocaleDateString()}
                    </small>
                  </td>
                  <td>
                    <small className="text-muted">
                      {card.expiresAt
                        ? new Date(card.expiresAt).toLocaleDateString()
                        : dash}
                    </small>
                  </td>
                  <td>
                    <Dropdown>
                      <Dropdown.Toggle
                        variant="link"
                        size="sm"
                        className="text-decoration-none p-0"
                      >
                        ⋯
                      </Dropdown.Toggle>
                      <Dropdown.Menu align="end">
                        {card.status === 'active' && (
                          <Dropdown.Item onClick={() => setSuspendTarget(card)}>
                            {t('marketing.cards.actions.suspend')}
                          </Dropdown.Item>
                        )}
                        {card.status === 'suspended' && (
                          <Dropdown.Item
                            onClick={() => setReinstateTarget(card)}
                          >
                            {t('marketing.cards.actions.reinstate')}
                          </Dropdown.Item>
                        )}
                        {card.status !== 'revoked' && (
                          <Dropdown.Item
                            className="text-danger"
                            onClick={() => setRevokeTarget(card)}
                          >
                            {t('marketing.cards.actions.revoke')}
                          </Dropdown.Item>
                        )}
                      </Dropdown.Menu>
                    </Dropdown>
                  </td>
                </tr>
              );
            })}
          </tbody>
        </Table>
      )}

      {issueOpen && (
        <IssueCardModal
          personId={personId}
          onClose={() => setIssueOpen(false)}
        />
      )}
      {suspendTarget && (
        <SuspendCardModal
          mode="suspend"
          card={suspendTarget}
          onClose={() => setSuspendTarget(null)}
        />
      )}
      {reinstateTarget && (
        <SuspendCardModal
          mode="reinstate"
          card={reinstateTarget}
          onClose={() => setReinstateTarget(null)}
        />
      )}
      {revokeTarget && (
        <RevokeCardModal
          card={revokeTarget}
          onClose={() => setRevokeTarget(null)}
        />
      )}
    </>
  );
};

export default CardsTab;
