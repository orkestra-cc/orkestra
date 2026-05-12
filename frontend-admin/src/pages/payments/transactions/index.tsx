import { useState } from 'react';
import { Badge, Button, Card, Form, Modal, Table } from 'react-bootstrap';
import PageHeader from 'components/common/PageHeader';
import IconButton from 'components/common/IconButton';
import Flex from 'components/common/Flex';
import {
  useListPaymentTransactionsQuery,
  useRefundPaymentTransactionMutation
} from 'store/api/paymentsApi';
import type { PaymentTransaction, TransactionStatus } from 'types/payments';

const statusColor: Record<TransactionStatus, string> = {
  pending: 'secondary',
  requires_action: 'warning',
  succeeded: 'success',
  failed: 'danger',
  refunded: 'info',
  partially_refunded: 'info'
};

const formatMoney = (cents: number, currency = 'EUR') =>
  new Intl.NumberFormat('it-IT', {
    style: 'currency',
    currency: currency.toUpperCase()
  }).format(cents / 100);

const TransactionsListPage: React.FC = () => {
  const [statusFilter, setStatusFilter] = useState('');
  const { data, isLoading, refetch } = useListPaymentTransactionsQuery({
    status: statusFilter || undefined
  });
  const [refund] = useRefundPaymentTransactionMutation();

  const [showRefund, setShowRefund] = useState(false);
  const [target, setTarget] = useState<PaymentTransaction | null>(null);
  const [refundCents, setRefundCents] = useState('');
  const [refundReason, setRefundReason] = useState('');

  const openRefund = (tx: PaymentTransaction) => {
    setTarget(tx);
    setRefundCents(String(tx.amountCents - (tx.refundedCents ?? 0)));
    setRefundReason('');
    setShowRefund(true);
  };

  const submitRefund = async () => {
    if (!target) return;
    await refund({
      id: target.uuid,
      input: { amountCents: Number(refundCents) || 0, reason: refundReason }
    }).unwrap();
    setShowRefund(false);
  };

  return (
    <>
      <PageHeader
        title="Transazioni"
        description="Storico degli addebiti e rimborsi Stripe"
        className="mb-3"
      >
        <Flex className="gap-2 mt-3">
          <IconButton
            icon="sync-alt"
            variant="orkestra-default"
            onClick={() => refetch()}
          >
            Aggiorna
          </IconButton>
        </Flex>
      </PageHeader>

      <Card className="mb-3">
        <Card.Body>
          <Form.Select
            value={statusFilter}
            onChange={e => setStatusFilter(e.target.value)}
            style={{ maxWidth: 260 }}
          >
            <option value="">Tutti gli stati</option>
            <option value="succeeded">Succeeded</option>
            <option value="failed">Failed</option>
            <option value="requires_action">Requires action</option>
            <option value="refunded">Refunded</option>
            <option value="partially_refunded">Partially refunded</option>
          </Form.Select>
        </Card.Body>
      </Card>

      <Card>
        <Card.Body className="p-0">
          {isLoading ? (
            <div className="p-4">Caricamento...</div>
          ) : !data?.items.length ? (
            <div className="p-4 text-muted text-center">
              Nessuna transazione.
            </div>
          ) : (
            <Table responsive hover className="mb-0">
              <thead className="bg-200">
                <tr>
                  <th>Provider</th>
                  <th>ID</th>
                  <th>Importo</th>
                  <th>Stato</th>
                  <th>Fattura</th>
                  <th>Data</th>
                  <th className="text-end">Azioni</th>
                </tr>
              </thead>
              <tbody>
                {data.items.map(tx => (
                  <tr key={tx.uuid}>
                    <td>
                      <Badge bg="dark">{tx.provider}</Badge>
                    </td>
                    <td>
                      <code className="fs--2">{tx.providerTxID || '—'}</code>
                    </td>
                    <td>
                      {formatMoney(tx.amountCents, tx.currency)}
                      {tx.refundedCents ? (
                        <div>
                          <small className="text-info">
                            -{formatMoney(tx.refundedCents, tx.currency)}{' '}
                            rimborsato
                          </small>
                        </div>
                      ) : null}
                    </td>
                    <td>
                      <Badge bg={statusColor[tx.status]}>{tx.status}</Badge>
                      {tx.failureMsg && (
                        <div>
                          <small className="text-danger">{tx.failureMsg}</small>
                        </div>
                      )}
                    </td>
                    <td>
                      {tx.invoiceUUID ? (
                        <code className="fs--2">
                          {tx.invoiceUUID.slice(0, 8)}
                        </code>
                      ) : (
                        '—'
                      )}
                    </td>
                    <td>{new Date(tx.createdAt).toLocaleString('it-IT')}</td>
                    <td className="text-end">
                      {(tx.status === 'succeeded' ||
                        tx.status === 'partially_refunded') &&
                        tx.providerTxID && (
                          <Button
                            size="sm"
                            variant="link"
                            className="text-warning"
                            onClick={() => openRefund(tx)}
                          >
                            Rimborsa
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

      <Modal show={showRefund} onHide={() => setShowRefund(false)}>
        <Modal.Header closeButton>
          <Modal.Title>Rimborso</Modal.Title>
        </Modal.Header>
        <Modal.Body>
          {target && (
            <>
              <p>
                Transazione <code>{target.providerTxID}</code> — originale{' '}
                <strong>
                  {formatMoney(target.amountCents, target.currency)}
                </strong>
              </p>
              <Form>
                <Form.Group className="mb-3">
                  <Form.Label>
                    Importo (centesimi — 0 per rimborso completo)
                  </Form.Label>
                  <Form.Control
                    type="number"
                    value={refundCents}
                    onChange={e => setRefundCents(e.target.value)}
                  />
                </Form.Group>
                <Form.Group>
                  <Form.Label>Motivo (facoltativo)</Form.Label>
                  <Form.Control
                    value={refundReason}
                    onChange={e => setRefundReason(e.target.value)}
                  />
                </Form.Group>
              </Form>
            </>
          )}
        </Modal.Body>
        <Modal.Footer>
          <Button variant="secondary" onClick={() => setShowRefund(false)}>
            Annulla
          </Button>
          <Button variant="warning" onClick={submitRefund}>
            Conferma rimborso
          </Button>
        </Modal.Footer>
      </Modal>
    </>
  );
};

export default TransactionsListPage;
