import { useSearchParams } from 'react-router-dom';
import { Card, Col, Row, Tab, Tabs, Alert } from 'react-bootstrap';
import { Trans, useTranslation } from 'react-i18next';
import { useAppSelector } from 'store/hooks';
import {
  selectCurrentOrgId,
  selectCurrentMembership
} from 'store/slices/tenantSlice';
import RolesTable from './RolesTable';
import BindingsTable from './BindingsTable';

/**
 * Role Management page — per-tenant view of roles and bindings backed by the
 * authz module. Requires a tenant context (X-Tenant-ID header) so you must
 * have selected a tenant in the top navbar before opening this page.
 */
const RoleManagementPage: React.FC = () => {
  const { t } = useTranslation();
  const [searchParams, setSearchParams] = useSearchParams();
  const tab = searchParams.get('tab') || 'roles';
  const currentOrgId = useAppSelector(selectCurrentOrgId);
  const currentMembership = useAppSelector(selectCurrentMembership);

  if (!currentOrgId) {
    return (
      <Row className="g-3">
        <Col xxl={12}>
          <Alert variant="warning">
            <Alert.Heading>{t('adminTenants.noOrgSelected')}</Alert.Heading>
            <p className="mb-0">{t('adminTenants.rolesNeedOrg')}</p>
          </Alert>
        </Col>
      </Row>
    );
  }

  return (
    <Row className="g-3">
      <Col xxl={12}>
        <Card>
          <Card.Header>
            <Row className="align-items-center">
              <Col>
                <h5 className="mb-1">{t('adminRoles.pageTitle')}</h5>
                <p className="text-muted small mb-0">
                  <Trans
                    i18nKey="adminRoles.intro"
                    values={{
                      tenant: currentMembership?.name ?? currentOrgId
                    }}
                    components={{ strong: <strong /> }}
                  />
                </p>
              </Col>
            </Row>
          </Card.Header>
          <Card.Body>
            <Tabs
              id="role-management-tabs"
              activeKey={tab}
              onSelect={k => {
                if (!k) return;
                setSearchParams(
                  prev => {
                    prev.set('tab', k);
                    return prev;
                  },
                  { replace: true }
                );
              }}
              className="mb-3"
            >
              <Tab eventKey="roles" title={t('adminRoles.tabs.roles')}>
                <RolesTable tenantId={currentOrgId} />
              </Tab>
              <Tab eventKey="bindings" title={t('adminRoles.tabs.bindings')}>
                <BindingsTable tenantId={currentOrgId} />
              </Tab>
            </Tabs>
          </Card.Body>
        </Card>
      </Col>
    </Row>
  );
};

export default RoleManagementPage;
