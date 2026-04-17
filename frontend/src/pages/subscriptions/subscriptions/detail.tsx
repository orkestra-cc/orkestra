import { Badge, Card, Nav, Tab, Table } from 'react-bootstrap';
import { useParams, useSearchParams } from 'react-router-dom';
import PageHeader from 'components/common/PageHeader';
import IconButton from 'components/common/IconButton';
import Flex from 'components/common/Flex';
import {
  useGetSubscriptionQuery,
  useListSubscriptionInvoicesQuery,
  useListSubscriptionActivityQuery,
  useCancelSubscriptionMutation,
  useReactivateSubscriptionMutation,
  useRetryChargeMutation,
} from 'store/api/subscriptionsApi';
import type { InvoiceStatus, SubStatus } from 'types/subscriptions';

const statusColor: Record<SubStatus, string> = {
  active: 'success',
  past_due: 'warning',
  suspended: 'danger',
  cancelled: 'secondary',
  expired: 'secondary',
};

const invoiceColor: Record<InvoiceStatus, string> = {
  pending: 'secondary',
  paid: 'success',
  failed: 'danger',
  refunded: 'info',
  void: 'secondary',
  awaiting_manual_payment: 'warning',
};

const formatMoney = (cents: number, currency = 'EUR') =>
  new Intl.NumberFormat('it-IT', { style: 'currency', currency }).format(cents / 100);

const SubscriptionDetailPage: React.FC = () => {
  const { id } = useParams<{ id: string }>();
  const [searchParams, setSearchParams] = useSearchParams();
  const activeTab = searchParams.get('tab') || 'overview';

  const { data: sub } = useGetSubscriptionQuery(id!, { skip: !id });
  const { data: invoices } = useListSubscriptionInvoicesQuery(id!, { skip: !id });
  const { data: activity } = useListSubscriptionActivityQuery({ id: id! }, { skip: !id });

  const [cancel] = useCancelSubscriptionMutation();
  const [reactivate] = useReactivateSubscriptionMutation();
  const [retry] = useRetryChargeMutation();

  if (!sub?.body) return <div>Caricamento…</div>;
  const s = sub.body;

  const setTab = (key: string) => {
    searchParams.set('tab', key);
    setSearchParams(searchParams, { replace: true });
  };

  return (
    <>
      <PageHeader
        title={`Sottoscrizione ${s.uuid.slice(0, 8)}`}
        description={`Ciclo: ${new Date(s.currentPeriodStart).toLocaleDateString('it-IT')} → ${new Date(s.currentPeriodEnd).toLocaleDateString('it-IT')}`}
        className="mb-3"
      >
        <Flex className="gap-2 mt-3 align-items-center">
          <Badge bg={statusColor[s.status]} className="fs--1">{s.status}</Badge>
          {s.status === 'active' && (
            <IconButton
              icon="times"
              variant="falcon-warning"
              size="sm"
              onClick={async () => {
                if (confirm('Cancellare a fine periodo?')) {
                  await cancel({ id: s.uuid, atPeriodEnd: true }).unwrap();
                }
              }}
            >
              Cancella a fine periodo
            </IconButton>
          )}
          {(s.status === 'past_due' || s.status === 'suspended') && (
            <>
              <IconButton
                icon="redo"
                variant="falcon-primary"
                size="sm"
                onClick={() => retry(s.uuid).unwrap()}
              >
                Riprova addebito
              </IconButton>
              <IconButton
                icon="play"
                variant="falcon-success"
                size="sm"
                onClick={() => reactivate(s.uuid).unwrap()}
              >
                Riattiva
              </IconButton>
            </>
          )}
        </Flex>
      </PageHeader>

      <Tab.Container activeKey={activeTab} onSelect={(k) => k && setTab(k)}>
        <Card className="mb-3">
          <Card.Header className="py-0 bg-light">
            <Nav variant="tabs" className="border-0">
              <Nav.Item><Nav.Link eventKey="overview">Overview</Nav.Link></Nav.Item>
              <Nav.Item><Nav.Link eventKey="invoices">Fatture ({invoices?.total ?? 0})</Nav.Link></Nav.Item>
              <Nav.Item><Nav.Link eventKey="activity">Attività ({activity?.total ?? 0})</Nav.Link></Nav.Item>
            </Nav>
          </Card.Header>
        </Card>

        <Tab.Content>
          <Tab.Pane eventKey="overview">
            <Card>
              <Card.Body>
                <dl className="row mb-0">
                  <dt className="col-sm-3">Cliente UUID</dt>
                  <dd className="col-sm-9"><code>{s.clientUUID}</code></dd>
                  <dt className="col-sm-3">Servizio UUID</dt>
                  <dd className="col-sm-9"><code>{s.serviceUUID}</code></dd>
                  <dt className="col-sm-3">Tier</dt>
                  <dd className="col-sm-9"><code>{s.tierCode}</code></dd>
                  <dt className="col-sm-3">Creata</dt>
                  <dd className="col-sm-9">{new Date(s.createdAt).toLocaleString('it-IT')}</dd>
                  <dt className="col-sm-3">Iniziata</dt>
                  <dd className="col-sm-9">{new Date(s.startedAt).toLocaleString('it-IT')}</dd>
                  <dt className="col-sm-3">Prossimo addebito</dt>
                  <dd className="col-sm-9">{new Date(s.nextBillingAt).toLocaleString('it-IT')}</dd>
                  <dt className="col-sm-3">Tentativi falliti</dt>
                  <dd className="col-sm-9">{s.failedChargeCount}</dd>
                  {s.cancelAtPeriodEnd && (
                    <>
                      <dt className="col-sm-3">Cancella a fine periodo</dt>
                      <dd className="col-sm-9 text-warning">Sì</dd>
                    </>
                  )}
                </dl>
              </Card.Body>
            </Card>
          </Tab.Pane>

          <Tab.Pane eventKey="invoices">
            <Card>
              <Card.Body className="p-0">
                {!invoices?.items.length ? (
                  <div className="p-4 text-muted text-center">Nessuna fattura generata.</div>
                ) : (
                  <Table responsive hover className="mb-0">
                    <thead className="bg-200">
                      <tr>
                        <th>Numero</th>
                        <th>Periodo</th>
                        <th>Totale</th>
                        <th>Stato</th>
                        <th>Emessa</th>
                        <th>Pagata</th>
                      </tr>
                    </thead>
                    <tbody>
                      {invoices.items.map((inv) => (
                        <tr key={inv.uuid}>
                          <td><code>{inv.number}</code></td>
                          <td>
                            {new Date(inv.periodStart).toLocaleDateString('it-IT')} →{' '}
                            {new Date(inv.periodEnd).toLocaleDateString('it-IT')}
                          </td>
                          <td>{formatMoney(inv.totalCents, inv.currency)}</td>
                          <td>
                            <Badge bg={invoiceColor[inv.status]}>{inv.status}</Badge>
                          </td>
                          <td>{new Date(inv.issuedAt).toLocaleDateString('it-IT')}</td>
                          <td>{inv.paidAt ? new Date(inv.paidAt).toLocaleDateString('it-IT') : '—'}</td>
                        </tr>
                      ))}
                    </tbody>
                  </Table>
                )}
              </Card.Body>
            </Card>
          </Tab.Pane>

          <Tab.Pane eventKey="activity">
            <Card>
              <Card.Body>
                {!activity?.items.length ? (
                  <div className="p-4 text-muted text-center">Nessun evento registrato.</div>
                ) : (
                  <ul className="list-unstyled mb-0">
                    {activity.items.map((a) => (
                      <li key={a.uuid} className="border-bottom py-2">
                        <Flex justifyContent="between">
                          <div>
                            <Badge bg="soft-info" className="me-2">{a.type}</Badge>
                            <strong>{a.message}</strong>
                            <div>
                              <small className="text-muted">
                                {new Date(a.createdAt).toLocaleString('it-IT')} · {a.actor}
                              </small>
                            </div>
                          </div>
                        </Flex>
                        {a.payload && Object.keys(a.payload).length > 0 && (
                          <pre className="mb-0 mt-1 fs--2 bg-light p-2 rounded">
                            {JSON.stringify(a.payload, null, 2)}
                          </pre>
                        )}
                      </li>
                    ))}
                  </ul>
                )}
              </Card.Body>
            </Card>
          </Tab.Pane>
        </Tab.Content>
      </Tab.Container>
    </>
  );
};

export default SubscriptionDetailPage;
