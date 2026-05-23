import { useState } from 'react';
import { Badge, Card, Form, Table } from 'react-bootstrap';
import { useTranslation } from 'react-i18next';
import PageHeader from 'components/common/PageHeader';
import IconButton from 'components/common/IconButton';
import Flex from 'components/common/Flex';
import { useListPaymentMethodsQuery } from 'store/api/paymentsApi';
import { useListAllOrgsAdminQuery } from 'store/api/tenantApi';

const PaymentMethodsPage: React.FC = () => {
  const { t } = useTranslation();
  const [tenantUUID, setTenantUUID] = useState('');
  const { data: tenantsData } = useListAllOrgsAdminQuery({ kind: 'external' });
  const { data, isLoading, refetch } = useListPaymentMethodsQuery(tenantUUID, {
    skip: !tenantUUID
  });

  return (
    <>
      <PageHeader
        title={t('payments.methods.title')}
        description={t('payments.methods.description')}
        className="mb-3"
      >
        <Flex className="gap-2 mt-3">
          <IconButton
            icon="sync-alt"
            variant="orkestra-default"
            onClick={() => refetch()}
          >
            {t('payments.methods.refresh')}
          </IconButton>
        </Flex>
      </PageHeader>

      <Card className="mb-3">
        <Card.Body>
          <Form.Label>{t('payments.methods.selectTenant')}</Form.Label>
          <Form.Select
            value={tenantUUID}
            onChange={e => setTenantUUID(e.target.value)}
          >
            <option value="">—</option>
            {tenantsData?.tenants.map(tenant => (
              <option key={tenant.id} value={tenant.id}>
                {tenant.name} ({tenant.slug})
              </option>
            ))}
          </Form.Select>
        </Card.Body>
      </Card>

      <Card>
        <Card.Body className="p-0">
          {!tenantUUID ? (
            <div className="p-4 text-muted text-center">
              {t('payments.methods.selectTenantHint')}
            </div>
          ) : isLoading ? (
            <div className="p-4">{t('payments.methods.loading')}</div>
          ) : !data?.items.length ? (
            <div className="p-4 text-muted text-center">
              {t('payments.methods.empty')}
            </div>
          ) : (
            <Table responsive hover className="mb-0">
              <thead className="bg-200">
                <tr>
                  <th>{t('payments.methods.columns.provider')}</th>
                  <th>{t('payments.methods.columns.brand')}</th>
                  <th>{t('payments.methods.columns.last4')}</th>
                  <th>{t('payments.methods.columns.expiry')}</th>
                  <th>{t('payments.methods.columns.default')}</th>
                  <th>{t('payments.methods.columns.created')}</th>
                </tr>
              </thead>
              <tbody>
                {data.items.map(pm => (
                  <tr key={pm.uuid}>
                    <td>
                      <Badge bg="dark">{pm.provider}</Badge>
                    </td>
                    <td>{pm.brand || '—'}</td>
                    <td>{pm.last4 ? `•••• ${pm.last4}` : '—'}</td>
                    <td>
                      {pm.expiryMonth && pm.expiryYear
                        ? `${pm.expiryMonth}/${pm.expiryYear}`
                        : '—'}
                    </td>
                    <td>
                      {pm.isDefault ? (
                        <Badge bg="success">
                          {t('payments.methods.defaultBadge')}
                        </Badge>
                      ) : (
                        '—'
                      )}
                    </td>
                    <td>
                      {new Date(pm.createdAt).toLocaleDateString('it-IT')}
                    </td>
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
