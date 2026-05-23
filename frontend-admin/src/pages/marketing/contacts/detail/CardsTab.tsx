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
import { Badge, Button, Dropdown, Spinner } from 'react-bootstrap';
import { useTranslation } from 'react-i18next';
import type { ColumnDef } from '@tanstack/react-table';

import useAdvanceTable from 'hooks/ui/useAdvanceTable';
import AdvanceTableProvider from 'providers/AdvanceTableProvider';
import AdvanceTable from 'components/common/advance-table/AdvanceTable';

import {
  useListCardTypesQuery,
  useListPersonCardsQuery
} from 'store/api/marketingApi';
import type {
  Card as MarketingCard,
  CardStatus,
  CardType
} from 'types/marketing';
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

  const cardTypesByUuid = useMemo<Record<string, CardType>>(
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

  const columns = useMemo<ColumnDef<MarketingCard>[]>(
    () => [
      {
        id: 'code',
        accessorKey: 'code',
        header: t('marketing.cards.col.code'),
        cell: ({ getValue }) => <code>{String(getValue() ?? '')}</code>,
        meta: { headerProps: { className: 'text-900' } }
      },
      {
        id: 'type',
        accessorFn: row =>
          cardTypesByUuid[row.cardTypeUuid]?.displayName ?? row.cardTypeUuid,
        header: t('marketing.cards.col.type'),
        cell: ({ row }) => {
          const type = cardTypesByUuid[row.original.cardTypeUuid];
          return (
            type?.displayName ?? (
              <code className="text-muted">
                {row.original.cardTypeUuid.slice(0, 8)}
              </code>
            )
          );
        },
        meta: { headerProps: { className: 'text-900' } }
      },
      {
        id: 'tier',
        accessorFn: row => row.tier ?? '',
        header: t('marketing.cards.col.tier'),
        cell: ({ getValue }) => (getValue() as string) || dash,
        meta: { headerProps: { className: 'text-900' } }
      },
      {
        id: 'status',
        accessorKey: 'status',
        header: t('marketing.cards.col.status'),
        cell: ({ getValue }) => {
          const status = getValue() as CardStatus;
          return (
            <Badge bg={statusVariant[status]} pill>
              {t(`marketing.cards.status.${status}`)}
            </Badge>
          );
        },
        meta: { headerProps: { className: 'text-900' } }
      },
      {
        id: 'issuedAt',
        accessorKey: 'issuedAt',
        header: t('marketing.cards.col.issuedAt'),
        cell: ({ getValue }) => (
          <small className="text-muted">
            {new Date(getValue() as string).toLocaleDateString()}
          </small>
        ),
        meta: { headerProps: { className: 'text-900' } }
      },
      {
        id: 'expiresAt',
        accessorKey: 'expiresAt',
        header: t('marketing.cards.col.expiresAt'),
        cell: ({ getValue }) => {
          const v = getValue() as string | undefined;
          return (
            <small className="text-muted">
              {v ? new Date(v).toLocaleDateString() : dash}
            </small>
          );
        },
        meta: { headerProps: { className: 'text-900' } }
      },
      {
        id: 'actions',
        enableSorting: false,
        header: () => <span className="d-block text-end">&nbsp;</span>,
        cell: ({ row }) => {
          const card = row.original;
          return (
            <div className="text-end">
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
                    <Dropdown.Item onClick={() => setReinstateTarget(card)}>
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
            </div>
          );
        },
        meta: {
          headerProps: { className: 'text-900 text-end', style: { width: 60 } }
        }
      }
    ],
    [t, cardTypesByUuid, dash]
  );

  const items = data?.items ?? [];
  const table = useAdvanceTable<MarketingCard>({
    data: items,
    columns,
    sortable: true,
    pagination: false,
    initialState: { sorting: [{ id: 'issuedAt', desc: true }] }
  });

  return (
    <>
      <div className="d-flex justify-content-end mb-3">
        <Button size="sm" variant="primary" onClick={() => setIssueOpen(true)}>
          {t('marketing.cards.actions.issue')}
        </Button>
      </div>

      {isLoading ? (
        <p className="text-muted mb-0">
          <Spinner animation="border" size="sm" className="me-2" />
          {t('marketing.cards.loading')}
        </p>
      ) : !items.length ? (
        <p className="text-muted mb-0">{t('marketing.cards.empty')}</p>
      ) : (
        <AdvanceTableProvider {...table}>
          <AdvanceTable
            headerClassName="bg-body-tertiary align-middle"
            rowClassName="align-middle"
            tableProps={{
              size: 'sm',
              className: 'fs-10 mb-0 overflow-hidden'
            }}
          />
        </AdvanceTableProvider>
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
