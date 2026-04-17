import { useState } from 'react';
import { Badge, Card, Form, Table } from 'react-bootstrap';
import PageHeader from 'components/common/PageHeader';
import IconButton from 'components/common/IconButton';
import Flex from 'components/common/Flex';
import { useListPaymentMethodsQuery } from 'store/api/paymentsApi';
import { useListSubscriptionClientsQuery } from 'store/api/subscriptionsApi';

const PaymentMethodsPage: React.FC = () => {
  const [clientUUID, setClientUUID] = useState('');
  const { data: clients } = useListSubscriptionClientsQuery(undefined);
  const { data, isLoading, refetch } = useListPaymentMethodsQuery(clientUUID, { skip: !clientUUID });

  return (
    <>
      <PageHeader title="Metodi di pagamento" description="Carte e metodi salvati per cliente" className="mb-3">
        <Flex className="gap-2 mt-3">
          <IconButton icon="sync" variant="falcon-default" onClick={() => refetch()}>
            Aggiorna
          </IconButton>
        </Flex>
      </PageHeader>

      <Card className="mb-3">
        <Card.Body>
          <Form.Label>Seleziona cliente</Form.Label>
          <Form.Select value={clientUUID} onChange={(e) => setClientUUID(e.target.value)}>
            <option value="">—</option>
            {clients?.items.map((c) => (
              <option key={c.uuid} value={c.uuid}>
                {c.displayName || c.legalName} ({c.email})
              </option>
            ))}
          </Form.Select>
        </Card.Body>
      </Card>

      <Card>
        <Card.Body className="p-0">
          {!clientUUID ? (
            <div className="p-4 text-muted text-center">Seleziona un cliente per visualizzare i metodi salvati.</div>
          ) : isLoading ? (
            <div className="p-4">Caricamento...</div>
          ) : !data?.items.length ? (
            <div className="p-4 text-muted text-center">Nessun metodo salvato per questo cliente.</div>
          ) : (
            <Table responsive hover className="mb-0">
              <thead className="bg-200">
                <tr>
                  <th>Provider</th>
                  <th>Brand</th>
                  <th>Ultime 4</th>
                  <th>Scadenza</th>
                  <th>Default</th>
                  <th>Creato</th>
                </tr>
              </thead>
              <tbody>
                {data.items.map((pm) => (
                  <tr key={pm.uuid}>
                    <td><Badge bg="dark">{pm.provider}</Badge></td>
                    <td>{pm.brand || '—'}</td>
                    <td>{pm.last4 ? `•••• ${pm.last4}` : '—'}</td>
                    <td>{pm.expiryMonth && pm.expiryYear ? `${pm.expiryMonth}/${pm.expiryYear}` : '—'}</td>
                    <td>{pm.isDefault ? <Badge bg="success">default</Badge> : '—'}</td>
                    <td>{new Date(pm.createdAt).toLocaleDateString('it-IT')}</td>
                  </tr>
                ))}
              </tbody>
            </Table>
          )}
        </Card.Body>
      </Card>
    </>
  );
};

export default PaymentMethodsPage;
