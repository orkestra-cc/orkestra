import { useState } from 'react';
import { Badge, Button, Card, Modal, Form, Table } from 'react-bootstrap';
import PageHeader from 'components/common/PageHeader';
import IconButton from 'components/common/IconButton';
import Flex from 'components/common/Flex';
import {
  useListSubscriptionServicesQuery,
  useCreateSubscriptionServiceMutation,
  useUpdateSubscriptionServiceMutation,
  useDeleteSubscriptionServiceMutation,
} from 'store/api/subscriptionsApi';
import type {
  CreateServiceInput,
  PricingTier,
  SubscriptionService,
} from 'types/subscriptions';

const emptyForm: CreateServiceInput = {
  code: '',
  name: '',
  category: 'workflow',
  description: '',
  active: true,
  setupFeeCents: 0,
  pricingTiers: [{ code: 'monthly', cycle: 'monthly', amountCents: 0, currency: 'EUR' }],
};

const formatMoney = (cents: number, currency = 'EUR') =>
  new Intl.NumberFormat('it-IT', { style: 'currency', currency }).format(cents / 100);

const ServicesListPage: React.FC = () => {
  const { data, isLoading, refetch } = useListSubscriptionServicesQuery(undefined);
  const [create] = useCreateSubscriptionServiceMutation();
  const [update] = useUpdateSubscriptionServiceMutation();
  const [del] = useDeleteSubscriptionServiceMutation();

  const [editing, setEditing] = useState<SubscriptionService | null>(null);
  const [showModal, setShowModal] = useState(false);
  const [form, setForm] = useState<CreateServiceInput>(emptyForm);

  const openNew = () => {
    setForm(emptyForm);
    setEditing(null);
    setShowModal(true);
  };
  const openEdit = (s: SubscriptionService) => {
    setForm({
      code: s.code,
      name: s.name,
      category: s.category,
      description: s.description,
      active: s.active,
      setupFeeCents: s.setupFeeCents,
      pricingTiers: s.pricingTiers,
    });
    setEditing(s);
    setShowModal(true);
  };

  const submit = async () => {
    if (editing) {
      await update({ id: editing.uuid, patch: form }).unwrap();
    } else {
      await create(form).unwrap();
    }
    setShowModal(false);
  };

  const updateTier = (i: number, patch: Partial<PricingTier>) => {
    const next = [...form.pricingTiers];
    next[i] = { ...next[i], ...patch };
    setForm({ ...form, pricingTiers: next });
  };
  const addTier = () =>
    setForm({
      ...form,
      pricingTiers: [
        ...form.pricingTiers,
        { code: 'annual', cycle: 'annual', amountCents: 0, currency: 'EUR' },
      ],
    });
  const removeTier = (i: number) =>
    setForm({ ...form, pricingTiers: form.pricingTiers.filter((_, idx) => idx !== i) });

  return (
    <>
      <PageHeader title="Servizi" description="Catalogo dei servizi AI offerti con prezzi ricorrenti" className="mb-3">
        <Flex className="gap-2 mt-3">
          <IconButton icon="plus" variant="primary" onClick={openNew}>
            Nuovo servizio
          </IconButton>
          <IconButton icon="sync" variant="falcon-default" onClick={() => refetch()}>
            Aggiorna
          </IconButton>
        </Flex>
      </PageHeader>

      <Card>
        <Card.Body className="p-0">
          {isLoading ? (
            <div className="p-4">Caricamento...</div>
          ) : !data?.items.length ? (
            <div className="p-4 text-muted text-center">Nessun servizio in catalogo. Creane uno per iniziare.</div>
          ) : (
            <Table responsive hover className="mb-0">
              <thead className="bg-200">
                <tr>
                  <th>Code</th>
                  <th>Nome</th>
                  <th>Categoria</th>
                  <th>Prezzi</th>
                  <th>Stato</th>
                  <th className="text-end">Azioni</th>
                </tr>
              </thead>
              <tbody>
                {data.items.map((s) => (
                  <tr key={s.uuid}>
                    <td><code>{s.code}</code></td>
                    <td>{s.name}</td>
                    <td>{s.category}</td>
                    <td>
                      {s.pricingTiers.map((t) => (
                        <div key={t.code}>
                          <small>
                            <strong>{t.cycle}</strong>: {formatMoney(t.amountCents, t.currency)}
                          </small>
                        </div>
                      ))}
                    </td>
                    <td>
                      <Badge bg={s.active ? 'success' : 'secondary'}>
                        {s.active ? 'Attivo' : 'Inattivo'}
                      </Badge>
                    </td>
                    <td className="text-end">
                      <Button size="sm" variant="link" onClick={() => openEdit(s)}>
                        Modifica
                      </Button>
                      <Button
                        size="sm"
                        variant="link"
                        className="text-danger"
                        onClick={async () => {
                          if (confirm(`Eliminare "${s.name}"?`)) {
                            await del(s.uuid).unwrap();
                          }
                        }}
                      >
                        Elimina
                      </Button>
                    </td>
                  </tr>
                ))}
              </tbody>
            </Table>
          )}
        </Card.Body>
      </Card>

      <Modal show={showModal} onHide={() => setShowModal(false)} size="lg">
        <Modal.Header closeButton>
          <Modal.Title>{editing ? 'Modifica servizio' : 'Nuovo servizio'}</Modal.Title>
        </Modal.Header>
        <Modal.Body>
          <Form>
            <div className="row g-3">
              <div className="col-md-4">
                <Form.Group>
                  <Form.Label>Code (SKU)</Form.Label>
                  <Form.Control
                    value={form.code}
                    disabled={!!editing}
                    onChange={(e) => setForm({ ...form, code: e.target.value })}
                    placeholder="es. n8n-workflow-pro"
                  />
                </Form.Group>
              </div>
              <div className="col-md-8">
                <Form.Group>
                  <Form.Label>Nome</Form.Label>
                  <Form.Control value={form.name} onChange={(e) => setForm({ ...form, name: e.target.value })} />
                </Form.Group>
              </div>
              <div className="col-md-6">
                <Form.Group>
                  <Form.Label>Categoria</Form.Label>
                  <Form.Select value={form.category} onChange={(e) => setForm({ ...form, category: e.target.value })}>
                    <option value="workflow">Workflow</option>
                    <option value="database">Database</option>
                    <option value="agent">Agent</option>
                    <option value="hosting">Hosting</option>
                    <option value="custom">Custom</option>
                  </Form.Select>
                </Form.Group>
              </div>
              <div className="col-md-6">
                <Form.Group>
                  <Form.Label>Setup fee (cents)</Form.Label>
                  <Form.Control
                    type="number"
                    value={form.setupFeeCents ?? 0}
                    onChange={(e) => setForm({ ...form, setupFeeCents: Number(e.target.value) })}
                  />
                </Form.Group>
              </div>
              <div className="col-12">
                <Form.Group>
                  <Form.Label>Descrizione</Form.Label>
                  <Form.Control
                    as="textarea"
                    rows={2}
                    value={form.description ?? ''}
                    onChange={(e) => setForm({ ...form, description: e.target.value })}
                  />
                </Form.Group>
              </div>
              <div className="col-12">
                <Form.Check
                  type="switch"
                  label="Attivo"
                  checked={form.active}
                  onChange={(e) => setForm({ ...form, active: e.target.checked })}
                />
              </div>
            </div>

            <hr />
            <Flex justifyContent="between" alignItems="center" className="mb-2">
              <strong>Tier di prezzo</strong>
              <Button size="sm" variant="falcon-default" onClick={addTier}>
                + Tier
              </Button>
            </Flex>
            {form.pricingTiers.map((t, i) => (
              <div className="row g-2 mb-2" key={i}>
                <div className="col-md-3">
                  <Form.Control
                    placeholder="code"
                    value={t.code}
                    onChange={(e) => updateTier(i, { code: e.target.value })}
                  />
                </div>
                <div className="col-md-3">
                  <Form.Select value={t.cycle} onChange={(e) => updateTier(i, { cycle: e.target.value as PricingTier['cycle'] })}>
                    <option value="monthly">Monthly</option>
                    <option value="quarterly">Quarterly</option>
                    <option value="annual">Annual</option>
                  </Form.Select>
                </div>
                <div className="col-md-3">
                  <Form.Control
                    type="number"
                    placeholder="amount (cents)"
                    value={t.amountCents}
                    onChange={(e) => updateTier(i, { amountCents: Number(e.target.value) })}
                  />
                </div>
                <div className="col-md-2">
                  <Form.Control
                    placeholder="EUR"
                    value={t.currency}
                    onChange={(e) => updateTier(i, { currency: e.target.value })}
                  />
                </div>
                <div className="col-md-1">
                  <Button variant="link" className="text-danger p-0" onClick={() => removeTier(i)} disabled={form.pricingTiers.length === 1}>
                    ✕
                  </Button>
                </div>
              </div>
            ))}
          </Form>
        </Modal.Body>
        <Modal.Footer>
          <Button variant="secondary" onClick={() => setShowModal(false)}>
            Annulla
          </Button>
          <Button variant="primary" onClick={submit}>
            Salva
          </Button>
        </Modal.Footer>
      </Modal>
    </>
  );
};

export default ServicesListPage;
