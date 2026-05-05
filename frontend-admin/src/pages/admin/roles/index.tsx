import { useSearchParams } from 'react-router-dom';
import { Card, Col, Row, Tab, Tabs, Alert } from 'react-bootstrap';
import { useAppSelector } from 'store/hooks';
import { selectCurrentOrgId, selectCurrentMembership } from 'store/slices/tenantSlice';
import RolesTable from './RolesTable';
import BindingsTable from './BindingsTable';

/**
 * Role Management page — per-tenant view of roles and bindings backed by the
 * authz module. Requires a tenant context (X-Tenant-ID header) so you must
 * have selected a tenant in the top navbar before opening this page.
 */
const RoleManagementPage: React.FC = () => {
  const [searchParams, setSearchParams] = useSearchParams();
  const tab = searchParams.get('tab') || 'roles';
  const currentOrgId = useAppSelector(selectCurrentOrgId);
  const currentMembership = useAppSelector(selectCurrentMembership);

  if (!currentOrgId) {
    return (
      <Row className="g-3">
        <Col xxl={12}>
          <Alert variant="warning">
            <Alert.Heading>No organization selected</Alert.Heading>
            <p className="mb-0">
              Role management is scoped to a single organization. Select or create
              one from the top-right org switcher to continue.
            </p>
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
                <h5 className="mb-1">Role Management</h5>
                <p className="text-muted small mb-0">
                  Manage roles and bindings for{' '}
                  <strong>{currentMembership?.name ?? currentOrgId}</strong>.
                  System roles are read-only; custom roles can be created by
                  picking permissions from the catalog.
                </p>
              </Col>
            </Row>
          </Card.Header>
          <Card.Body>
            <Tabs
              id="role-management-tabs"
              activeKey={tab}
              onSelect={(k) => {
                if (!k) return;
                setSearchParams((prev) => { prev.set('tab', k); return prev; }, { replace: true });
              }}
              className="mb-3"
            >
              <Tab eventKey="roles" title="Roles">
                <RolesTable tenantId={currentOrgId} />
              </Tab>
              <Tab eventKey="bindings" title="Role Bindings">
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
