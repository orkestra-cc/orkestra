import { useState } from 'react';
import { Badge, Button, Card, Form, Modal, Table } from 'react-bootstrap';
import { Trans, useTranslation } from 'react-i18next';
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
  const { t } = useTranslation();
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
        title={t('payments.transactions.title')}
        description={t('payments.transactions.description')}
        className="mb-3"
      >
        <Flex className="gap-2 mt-3">
          <IconButton
            icon="sync-alt"
            variant="orkestra-default"
            onClick={() => refetch()}
          >
            {t('payments.transactions.refresh')}
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
            <option value="">{t('payments.transactions.filters.all')}</option>
            <option value="succeeded">
              {t('payments.transactions.filters.succeeded')}
            </option>
            <option value="failed">
              {t('payments.transactions.filters.failed')}
            </option>
            <option value="requires_action">
              {t('payments.transactions.filters.requires_action')}
            </option>
            <option value="refunded">
              {t('payments.transactions.filters.refunded')}
            </option>
            <option value="partially_refunded">
              {t('payments.transactions.filters.partially_refunded')}
            </option>
          </Form.Select>
        </Card.Body>
      </Card>

      <Card>
        <Card.Body className="p-0">
          {isLoading ? (
            <div className="p-4">{t('payments.transactions.loading')}</div>
          ) : !data?.items.length ? (
            <div className="p-4 text-muted text-center">
              {t('payments.transactions.empty')}
            </div>
          ) : (
            <Table responsive hover className="mb-0">
              <thead className="bg-200">
                <tr>
                  <th>{t('payments.transactions.columns.provider')}</th>
                  <th>{t('payments.transactions.columns.id')}</th>
                  <th>{t('payments.transactions.columns.amount')}</th>
                  <th>{t('payments.transactions.columns.status')}</th>
                  <th>{t('payments.transactions.columns.invoice')}</th>
                  <th>{t('payments.transactions.columns.date')}</th>
                  <th className="text-end">
                    {t('payments.transactions.columns.actions')}
                  </th>
                </tr>
              </thead>
              <tbody>
                {data.items.map(tx => (
                  <tr key={tx.uuid}>
                    <td>
                      <Badge bg="dark">{tx.provider}</Badge>
                    </td>
                    <td>
                      <code className="fs--2">
                        {tx.providerTxID || t('payments.transactions.dash')}
                      </code>
                    </td>
                    <td>
                      {formatMoney(tx.amountCents, tx.currency)}
                      {tx.refundedCents ? (
                        <div>
                          <small className="text-info">
                            -{formatMoney(tx.refundedCents, tx.currency)}{' '}
                            {t('payments.transactions.refundedSuffix')}
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
                        t('payments.transactions.dash')
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
                            {t('payments.transactions.refundButton')}
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
          <Modal.Title>
            {t('payments.transactions.refundModal.title')}
          </Modal.Title>
        </Modal.Header>
        <Modal.Body>
          {target && (
            <>
              <p>
                <Trans
                  i18nKey="payments.transactions.refundModal.intro"
                  values={{
                    providerTxID: target.providerTxID,
                    amount: formatMoney(target.amountCents, target.currency)
                  }}
                  components={{ code: <code />, strong: <strong /> }}
                />
              </p>
              <Form>
                <Form.Group className="mb-3">
                  <Form.Label>
                    {t('payments.transactions.refundModal.amountLabel')}
                  </Form.Label>
                  <Form.Control
                    type="number"
                    value={refundCents}
                    onChange={e => setRefundCents(e.target.value)}
                  />
                </Form.Group>
                <Form.Group>
                  <Form.Label>
                    {t('payments.transactions.refundModal.reasonLabel')}
                  </Form.Label>
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
            {t('payments.transactions.refundModal.cancel')}
          </Button>
          <Button variant="warning" onClick={submitRefund}>
            {t('payments.transactions.refundModal.confirm')}
          </Button>
        </Modal.Footer>
      </Modal>
    </>
  );
};

export default TransactionsListPage;
