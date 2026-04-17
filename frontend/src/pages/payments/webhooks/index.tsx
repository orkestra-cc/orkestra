import { useState } from 'react';
import { Badge, Card, Form, Table } from 'react-bootstrap';
import PageHeader from 'components/common/PageHeader';
import IconButton from 'components/common/IconButton';
import Flex from 'components/common/Flex';
import { useListPaymentWebhookEventsQuery } from 'store/api/paymentsApi';

const WebhookEventsPage: React.FC = () => {
  const [provider, setProvider] = useState('');
  const { data, isLoading, refetch } = useListPaymentWebhookEventsQuery({ provider: provider || undefined });

  return (
    <>
      <PageHeader title="Webhook events" description="Audit trail dei webhook ricevuti dai gateway" className="mb-3">
        <Flex className="gap-2 mt-3">
          <IconButton icon="sync" variant="falcon-default" onClick={() => refetch()}>
            Aggiorna
          </IconButton>
        </Flex>
      </PageHeader>

      <Card className="mb-3">
        <Card.Body>
          <Form.Select value={provider} onChange={(e) => setProvider(e.target.value)} style={{ maxWidth: 200 }}>
            <option value="">Tutti i provider</option>
            <option value="stripe">Stripe</option>
            <option value="paypal">PayPal</option>
          </Form.Select>
        </Card.Body>
      </Card>

      <Card>
        <Card.Body className="p-0">
          {isLoading ? (
            <div className="p-4">Caricamento...</div>
          ) : !data?.items.length ? (
            <div className="p-4 text-muted text-center">Nessun evento ricevuto.</div>
          ) : (
            <Table responsive hover className="mb-0">
              <thead className="bg-200">
                <tr>
                  <th>Provider</th>
                  <th>Event ID</th>
                  <th>Tipo</th>
                  <th>Normalizzato</th>
                  <th>Processato</th>
                  <th>Ricevuto</th>
                </tr>
              </thead>
              <tbody>
                {data.items.map((e) => (
                  <tr key={e.uuid}>
                    <td><Badge bg="dark">{e.provider}</Badge></td>
                    <td><code className="fs--2">{e.providerEventID}</code></td>
                    <td><code className="fs--2">{e.type}</code></td>
                    <td>{e.normalized || <span className="text-muted">—</span>}</td>
                    <td>
                      {e.processed ? (
                        <Badge bg="success">ok</Badge>
                      ) : e.processError ? (
                        <span>
                          <Badge bg="danger">errore</Badge>
                          <div><small className="text-danger">{e.processError}</small></div>
                        </span>
                      ) : (
                        <Badge bg="secondary">in attesa</Badge>
                      )}
                    </td>
                    <td>{new Date(e.receivedAt).toLocaleString('it-IT')}</td>
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

export default WebhookEventsPage;
