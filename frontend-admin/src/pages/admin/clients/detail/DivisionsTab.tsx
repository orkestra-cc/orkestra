import { useState } from 'react';
import { Alert, Button, Card, Spinner, Table } from 'react-bootstrap';
import { Link } from 'react-router';
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome';
import { Trans, useTranslation } from 'react-i18next';
import SubtleBadge from 'components/common/SubtleBadge';
import type { Org } from 'store/api/tenantApi';
import { useListTenantDivisionsAdminQuery } from 'store/api/tenantApi';
import CreateDivisionModal from './CreateDivisionModal';

interface Props {
  org: Org;
}

/**
 * Divisions tab — lists direct children (ParentTenantUUID=this) and lets
 * an admin add new divisions. Each division is itself a Tier-2 Tenant
 * (Kind=external) with its own subscriptions and members. Entitlement
 * cascade across the hierarchy is out of scope for this iteration — each
 * division gets its own subscription and capability grants.
 */
const DivisionsTab: React.FC<Props> = ({ org }) => {
  const { t } = useTranslation();
  const { data, isLoading, error } = useListTenantDivisionsAdminQuery(org.id);
  const [showCreate, setShowCreate] = useState(false);

  const divisions = data?.divisions ?? [];

  return (
    <>
      <Alert variant="light" className="fs-10 py-2 border">
        <FontAwesomeIcon icon="info-circle" className="me-2 text-info" />
        <Trans
          i18nKey="adminClients.divisions.intro"
          components={{ strong: <strong /> }}
        />
      </Alert>

      <Card className="shadow-none border-0">
        <Card.Header className="d-flex justify-content-between align-items-center px-0 py-2">
          <h5 className="mb-0 fs-9">
            {t(
              divisions.length === 1
                ? 'adminClients.divisions.countOne'
                : 'adminClients.divisions.countOther',
              { count: divisions.length }
            )}
          </h5>
          <Button
            variant="primary"
            size="sm"
            onClick={() => setShowCreate(true)}
          >
            <FontAwesomeIcon icon="plus" className="me-2" />
            {t('adminClients.divisions.addButton')}
          </Button>
        </Card.Header>
        <Card.Body className="p-0">
          {isLoading ? (
            <div className="text-center py-4">
              <Spinner animation="border" size="sm" />
            </div>
          ) : error ? (
            <Alert variant="danger" className="fs-10">
              {t('adminClients.divisions.loadFailed')}
            </Alert>
          ) : (
            <Table size="sm" className="fs-10 mb-0">
              <thead className="bg-body-tertiary">
                <tr>
                  <th>{t('adminClients.divisions.colName')}</th>
                  <th>{t('adminClients.divisions.colSlug')}</th>
                  <th>{t('adminClients.divisions.colPlan')}</th>
                  <th>{t('adminClients.divisions.colStatus')}</th>
                  <th>{t('adminClients.divisions.colCreated')}</th>
                </tr>
              </thead>
              <tbody>
                {divisions.map(d => (
                  <tr key={d.id} className="align-middle">
                    <td>
                      <Link to={`/admin/clients/${d.id}`}>{d.name}</Link>
                    </td>
                    <td className="text-muted">
                      <code className="fs-11">{d.slug}</code>
                    </td>
                    <td>
                      <SubtleBadge bg="secondary" pill>
                        {d.plan}
                      </SubtleBadge>
                    </td>
                    <td>
                      <SubtleBadge bg="success" pill>
                        {d.status ??
                          t('adminClients.divisions.statusActiveFallback')}
                      </SubtleBadge>
                    </td>
                    <td className="text-muted">
                      {new Date(d.createdAt).toLocaleDateString()}
                    </td>
                  </tr>
                ))}
                {divisions.length === 0 && (
                  <tr>
                    <td colSpan={5} className="text-center text-muted py-4">
                      {t('adminClients.divisions.empty')}
                    </td>
                  </tr>
                )}
              </tbody>
            </Table>
          )}
        </Card.Body>
      </Card>

      <CreateDivisionModal
        parentId={org.id}
        parentName={org.name}
        show={showCreate}
        onHide={() => setShowCreate(false)}
      />
    </>
  );
};

export default DivisionsTab;
