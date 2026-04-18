import { useMemo, useState } from 'react';
import { Badge, Button, Card, Form, Modal, Table } from 'react-bootstrap';
import { Link } from 'react-router-dom';
import PageHeader from 'components/common/PageHeader';
import IconButton from 'components/common/IconButton';
import Flex from 'components/common/Flex';
import {
  useListSubscriptionsQuery,
  useListSubscriptionClientsQuery,
  useListSubscriptionServicesQuery,
  useCreateSubscriptionMutation,
} from 'store/api/subscriptionsApi';
import type { SubStatus } from 'types/subscriptions';

const statusColor: Record<SubStatus, string> = {
  active: 'success',
  past_due: 'warning',
  suspended: 'danger',
  cancelled: 'secondary',
  expired: 'secondary',
};

const SubscriptionsListPage: React.FC = () => {
  const [statusFilter, setStatusFilter] = useState<string>('');
  const { data, isLoading, refetch } = useListSubscriptionsQuery({ status: statusFilter || undefined });
  const { data: clients } = useListSubscriptionClientsQuery(undefined);
  const { data: services } = useListSubscriptionServicesQuery(undefined);
  const [create] = useCreateSubscriptionMutation();

  const [showModal, setShowModal] = useState(false);
  const [form, setForm] = useState({ clientUUID: '', serviceUUID: '', tierCode: '' });

  const clientById = useMemo(() => {
    const m = new Map<string, string>();
    clients?.items.forEach((c) => m.set(c.uuid, c.displayName || c.legalName));
    return m;
  }, [clients]);
  const serviceById = useMemo(() => {
    const m = new Map<string, string>();
    services?.items.forEach((s) => m.set(s.uuid, s.name));
    return m;
  }, [services]);

  const selectedService = services?.items.find((s) => s.uuid === form.serviceUUID);

  const submit = async () => {
    await create(form).unwrap();
    setShowModal(false);
    setForm({ clientUUID: '', serviceUUID: '', tierCode: '' });
  };

  return (
    <>
      <PageHeader title="Sottoscrizioni" description="Clienti × servizi con ciclo di rinnovo automatico" className="mb-3">
        <Flex className="gap-2 mt-3">
          <IconButton
            icon="plus"
            variant="primary"
            onClick={() => setShowModal(true)}
            disabled={!clients?.items.length || !services?.items.length}
          >
            Nuova sottoscrizione
          </IconButton>
          <IconButton icon="sync-alt" variant="falcon-default" onClick={() => refetch()}>
            Aggiorna
          </IconButton>
        </Flex>
      </PageHeader>

      <Card className="mb-3">
        <Card.Body>
          <Form.Select value={statusFilter} onChange={(e) => setStatusFilter(e.target.value)} style={{ maxWidth: 220 }}>
            <option value="">Tutti gli stati</option>
            <option value="active">Attive</option>
            <option value="past_due">In ritardo</option>
            <option value="suspended">Sospese</option>
            <option value="cancelled">Cancellate</option>
            <option value="expired">Scadute</option>
          </Form.Select>
        </Card.Body>
      </Card>

      <Card>
        <Card.Body className="p-0">
          {isLoading ? (
            <div className="p-4">Caricamento...</div>
          ) : !data?.items.length ? (
            <div className="p-4 text-muted text-center">Nessuna sottoscrizione.</div>
          ) : (
            <Table responsive hover className="mb-0">
              <thead className="bg-200">
                <tr>
                  <th>Cliente</th>
                  <th>Servizio</th>
                  <th>Tier</th>
                  <th>Stato</th>
                  <th>Prossimo addebito</th>
                  <th>Tentativi falliti</th>
                  <th className="text-end">Azioni</th>
                </tr>
              </thead>
              <tbody>
                {data.items.map((s) => (
                  <tr key={s.uuid}>
                    <td>{clientById.get(s.clientUUID) ?? s.clientUUID.slice(0, 8)}</td>
                    <td>{serviceById.get(s.serviceUUID) ?? s.serviceUUID.slice(0, 8)}</td>
                    <td><code>{s.tierCode}</code></td>
                    <td>
                      <Badge bg={statusColor[s.status]}>{s.status}</Badge>
                    </td>
                    <td>{new Date(s.nextBillingAt).toLocaleDateString('it-IT')}</td>
                    <td>{s.failedChargeCount}</td>
                    <td className="text-end">
                      <Link to={`/subscriptions/subscriptions/${s.uuid}`}>Dettagli</Link>
                    </td>
                  </tr>
                ))}
              </tbody>
            </Table>
          )}
        </Card.Body>
      </Card>

      <Modal show={showModal} onHide={() => setShowModal(false)}>
        <Modal.Header closeButton>
          <Modal.Title>Nuova sottoscrizione</Modal.Title>
        </Modal.Header>
        <Modal.Body>
          <Form>
            <Form.Group className="mb-3">
              <Form.Label>Cliente</Form.Label>
              <Form.Select
                value={form.clientUUID}
                onChange={(e) => setForm({ ...form, clientUUID: e.target.value })}
              >
                <option value="">Seleziona...</option>
                {clients?.items
                  .filter((c) => c.status === 'active')
                  .map((c) => (
                    <option key={c.uuid} value={c.uuid}>
                      {c.displayName || c.legalName} ({c.email})
                    </option>
                  ))}
              </Form.Select>
            </Form.Group>
            <Form.Group className="mb-3">
              <Form.Label>Servizio</Form.Label>
              <Form.Select
                value={form.serviceUUID}
                onChange={(e) => setForm({ ...form, serviceUUID: e.target.value, tierCode: '' })}
              >
                <option value="">Seleziona...</option>
                {services?.items
                  .filter((s) => s.active)
                  .map((s) => (
                    <option key={s.uuid} value={s.uuid}>
                      {s.name} ({s.code})
                    </option>
                  ))}
              </Form.Select>
            </Form.Group>
            {selectedService && (
              <Form.Group>
                <Form.Label>Tier di prezzo</Form.Label>
                <Form.Select
                  value={form.tierCode}
                  onChange={(e) => setForm({ ...form, tierCode: e.target.value })}
                >
                  <option value="">Seleziona...</option>
                  {selectedService.pricingTiers.map((t) => (
                    <option key={t.code} value={t.code}>
                      {t.code} — {t.cycle} — {(t.amountCents / 100).toFixed(2)} {t.currency}
                    </option>
                  ))}
                </Form.Select>
              </Form.Group>
            )}
          </Form>
        </Modal.Body>
        <Modal.Footer>
          <Button variant="secondary" onClick={() => setShowModal(false)}>
            Annulla
          </Button>
          <Button
            variant="primary"
            disabled={!form.clientUUID || !form.serviceUUID || !form.tierCode}
            onClick={submit}
          >
            Crea
          </Button>
        </Modal.Footer>
      </Modal>
    </>
  );
};

export default SubscriptionsListPage;
