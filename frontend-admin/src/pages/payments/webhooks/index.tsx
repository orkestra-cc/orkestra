import { useState } from 'react';
import { Badge, Card, Form, Table } from 'react-bootstrap';
import { useTranslation } from 'react-i18next';
import PageHeader from 'components/common/PageHeader';
import IconButton from 'components/common/IconButton';
import Flex from 'components/common/Flex';
import { useListPaymentWebhookEventsQuery } from 'store/api/paymentsApi';

const WebhookEventsPage: React.FC = () => {
  const { t } = useTranslation();
  const [provider, setProvider] = useState('');
  const { data, isLoading, refetch } = useListPaymentWebhookEventsQuery({
    provider: provider || undefined
  });

  return (
    <>
      <PageHeader
        title={t('payments.webhooks.title')}
        description={t('payments.webhooks.description')}
        className="mb-3"
      >
        <Flex className="gap-2 mt-3">
          <IconButton
            icon="sync-alt"
            variant="orkestra-default"
            onClick={() => refetch()}
          >
            {t('payments.webhooks.refresh')}
          </IconButton>
        </Flex>
      </PageHeader>

      <Card className="mb-3">
        <Card.Body>
          <Form.Select
            value={provider}
            onChange={e => setProvider(e.target.value)}
            style={{ maxWidth: 200 }}
          >
            <option value="">{t('payments.webhooks.allProviders')}</option>
            <option value="stripe">Stripe</option>
            <option value="paypal">PayPal</option>
          </Form.Select>
        </Card.Body>
      </Card>

      <Card>
        <Card.Body className="p-0">
          {isLoading ? (
            <div className="p-4">{t('payments.webhooks.loading')}</div>
          ) : !data?.items.length ? (
            <div className="p-4 text-muted text-center">
              {t('payments.webhooks.empty')}
            </div>
          ) : (
            <Table responsive hover className="mb-0">
              <thead className="bg-200">
                <tr>
                  <th>{t('payments.webhooks.columns.provider')}</th>
                  <th>{t('payments.webhooks.columns.eventId')}</th>
                  <th>{t('payments.webhooks.columns.type')}</th>
                  <th>{t('payments.webhooks.columns.normalized')}</th>
                  <th>{t('payments.webhooks.columns.processed')}</th>
                  <th>{t('payments.webhooks.columns.received')}</th>
                </tr>
              </thead>
              <tbody>
                {data.items.map(evt => (
                  <tr key={evt.uuid}>
                    <td>
                      <Badge bg="dark">{evt.provider}</Badge>
                    </td>
                    <td>
                      <code className="fs--2">{evt.providerEventID}</code>
                    </td>
                    <td>
                      <code className="fs--2">{evt.type}</code>
                    </td>
                    <td>
                      {evt.normalized || <span className="text-muted">—</span>}
                    </td>
                    <td>
                      {evt.processed ? (
                        <Badge bg="success">
                          {t('payments.webhooks.statusOk')}
                        </Badge>
                      ) : evt.processError ? (
                        <span>
                          <Badge bg="danger">
                            {t('payments.webhooks.statusError')}
                          </Badge>
                          <div>
                            <small className="text-danger">
                              {evt.processError}
                            </small>
                          </div>
                        </span>
                      ) : (
                        <Badge bg="secondary">
                          {t('payments.webhooks.statusPending')}
                        </Badge>
                      )}
                    </td>
                    <td>{new Date(evt.receivedAt).toLocaleString('it-IT')}</td>
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
