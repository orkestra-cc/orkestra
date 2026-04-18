import { useState } from 'react';
import { Badge, Button, Card, Form, Modal, Table } from 'react-bootstrap';
import PageHeader from 'components/common/PageHeader';
import IconButton from 'components/common/IconButton';
import Flex from 'components/common/Flex';
import {
  useListSubscriptionClientsQuery,
  useCreateSubscriptionClientMutation,
  useUpdateSubscriptionClientMutation,
  useArchiveSubscriptionClientMutation,
} from 'store/api/subscriptionsApi';
import type { CreateClientInput, SubscriptionClient } from 'types/subscriptions';

const emptyForm: CreateClientInput = {
  legalName: '',
  displayName: '',
  email: '',
  vatNumber: '',
  fiscalCode: '',
  billingAddr: { country: 'IT' },
  notes: '',
};

const ClientsListPage: React.FC = () => {
  const [search, setSearch] = useState('');
  const { data, isLoading, refetch } = useListSubscriptionClientsQuery({ search });
  const [create] = useCreateSubscriptionClientMutation();
  const [update] = useUpdateSubscriptionClientMutation();
  const [archive] = useArchiveSubscriptionClientMutation();

  const [showModal, setShowModal] = useState(false);
  const [editing, setEditing] = useState<SubscriptionClient | null>(null);
  const [form, setForm] = useState<CreateClientInput>(emptyForm);

  const openNew = () => {
    setForm(emptyForm);
    setEditing(null);
    setShowModal(true);
  };
  const openEdit = (c: SubscriptionClient) => {
    setForm({
      legalName: c.legalName,
      displayName: c.displayName,
      email: c.email,
      vatNumber: c.vatNumber ?? '',
      fiscalCode: c.fiscalCode ?? '',
      billingAddr: c.billingAddr ?? {},
      notes: c.notes ?? '',
    });
    setEditing(c);
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

  return (
    <>
      <PageHeader title="Clienti" description="Aziende e persone a cui vendi i servizi" className="mb-3">
        <Flex className="gap-2 mt-3">
          <IconButton icon="plus" variant="primary" onClick={openNew}>
            Nuovo cliente
          </IconButton>
          <IconButton icon="sync-alt" variant="falcon-default" onClick={() => refetch()}>
            Aggiorna
          </IconButton>
        </Flex>
      </PageHeader>

      <Card className="mb-3">
        <Card.Body>
          <Form.Control
            type="search"
            placeholder="Cerca per nome, email, P.IVA…"
            value={search}
            onChange={(e) => setSearch(e.target.value)}
          />
        </Card.Body>
      </Card>

      <Card>
        <Card.Body className="p-0">
          {isLoading ? (
            <div className="p-4">Caricamento...</div>
          ) : !data?.items.length ? (
            <div className="p-4 text-muted text-center">Nessun cliente. Aggiungine uno per iniziare.</div>
          ) : (
            <Table responsive hover className="mb-0">
              <thead className="bg-200">
                <tr>
                  <th>Denominazione</th>
                  <th>Email</th>
                  <th>P.IVA</th>
                  <th>Stato</th>
                  <th className="text-end">Azioni</th>
                </tr>
              </thead>
              <tbody>
                {data.items.map((c) => (
                  <tr key={c.uuid}>
                    <td>
                      <strong>{c.displayName || c.legalName}</strong>
                      {c.displayName && c.displayName !== c.legalName && (
                        <div><small className="text-muted">{c.legalName}</small></div>
                      )}
                    </td>
                    <td>{c.email}</td>
                    <td>{c.vatNumber || '—'}</td>
                    <td>
                      <Badge bg={c.status === 'active' ? 'success' : 'secondary'}>{c.status}</Badge>
                    </td>
                    <td className="text-end">
                      <Button size="sm" variant="link" onClick={() => openEdit(c)}>
                        Modifica
                      </Button>
                      {c.status === 'active' && (
                        <Button
                          size="sm"
                          variant="link"
                          className="text-danger"
                          onClick={async () => {
                            if (confirm(`Archiviare "${c.displayName || c.legalName}"?`)) {
                              await archive(c.uuid).unwrap();
                            }
                          }}
                        >
                          Archivia
                        </Button>
                      )}
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
          <Modal.Title>{editing ? 'Modifica cliente' : 'Nuovo cliente'}</Modal.Title>
        </Modal.Header>
        <Modal.Body>
          <Form>
            <div className="row g-3">
              <div className="col-md-6">
                <Form.Group>
                  <Form.Label>Ragione sociale *</Form.Label>
                  <Form.Control
                    value={form.legalName}
                    onChange={(e) => setForm({ ...form, legalName: e.target.value })}
                  />
                </Form.Group>
              </div>
              <div className="col-md-6">
                <Form.Group>
                  <Form.Label>Nome visualizzato</Form.Label>
                  <Form.Control
                    value={form.displayName ?? ''}
                    onChange={(e) => setForm({ ...form, displayName: e.target.value })}
                  />
                </Form.Group>
              </div>
              <div className="col-md-6">
                <Form.Group>
                  <Form.Label>Email *</Form.Label>
                  <Form.Control
                    type="email"
                    value={form.email}
                    onChange={(e) => setForm({ ...form, email: e.target.value })}
                  />
                </Form.Group>
              </div>
              <div className="col-md-3">
                <Form.Group>
                  <Form.Label>P.IVA</Form.Label>
                  <Form.Control value={form.vatNumber ?? ''} onChange={(e) => setForm({ ...form, vatNumber: e.target.value })} />
                </Form.Group>
              </div>
              <div className="col-md-3">
                <Form.Group>
                  <Form.Label>Codice fiscale</Form.Label>
                  <Form.Control value={form.fiscalCode ?? ''} onChange={(e) => setForm({ ...form, fiscalCode: e.target.value })} />
                </Form.Group>
              </div>
              <div className="col-12"><strong>Indirizzo di fatturazione</strong></div>
              <div className="col-md-8">
                <Form.Group>
                  <Form.Label>Indirizzo</Form.Label>
                  <Form.Control
                    value={form.billingAddr?.line1 ?? ''}
                    onChange={(e) => setForm({ ...form, billingAddr: { ...form.billingAddr, line1: e.target.value } })}
                  />
                </Form.Group>
              </div>
              <div className="col-md-4">
                <Form.Group>
                  <Form.Label>Città</Form.Label>
                  <Form.Control
                    value={form.billingAddr?.city ?? ''}
                    onChange={(e) => setForm({ ...form, billingAddr: { ...form.billingAddr, city: e.target.value } })}
                  />
                </Form.Group>
              </div>
              <div className="col-md-2">
                <Form.Group>
                  <Form.Label>Prov.</Form.Label>
                  <Form.Control
                    value={form.billingAddr?.province ?? ''}
                    onChange={(e) => setForm({ ...form, billingAddr: { ...form.billingAddr, province: e.target.value } })}
                  />
                </Form.Group>
              </div>
              <div className="col-md-3">
                <Form.Group>
                  <Form.Label>CAP</Form.Label>
                  <Form.Control
                    value={form.billingAddr?.postalCode ?? ''}
                    onChange={(e) => setForm({ ...form, billingAddr: { ...form.billingAddr, postalCode: e.target.value } })}
                  />
                </Form.Group>
              </div>
              <div className="col-md-3">
                <Form.Group>
                  <Form.Label>Paese</Form.Label>
                  <Form.Control
                    value={form.billingAddr?.country ?? 'IT'}
                    onChange={(e) => setForm({ ...form, billingAddr: { ...form.billingAddr, country: e.target.value } })}
                  />
                </Form.Group>
              </div>
              <div className="col-12">
                <Form.Group>
                  <Form.Label>Note</Form.Label>
                  <Form.Control
                    as="textarea"
                    rows={2}
                    value={form.notes ?? ''}
                    onChange={(e) => setForm({ ...form, notes: e.target.value })}
                  />
                </Form.Group>
              </div>
            </div>
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

export default ClientsListPage;
