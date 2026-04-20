import { Card, Col, Row } from 'react-bootstrap';
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome';
import IdPConfigForm from './IdPConfigForm';
import ScimTokenSection from './ScimTokenSection';

// /identity is tenant-scoped — every call flows under the current
// X-Tenant-ID. Operators who want to configure a different tenant switch
// tenants via the tenant switcher first. Gated by tenant.update on the
// backend (see identity/module.go::RegisterRoutes).
const IdentityAdminPage: React.FC = () => {
  return (
    <>
      <Row className="g-3 mb-3">
        <Col>
          <Card className="shadow-none border">
            <Card.Body>
              <h5 className="mb-1">
                <FontAwesomeIcon icon="id-card" className="me-2 text-primary" />
                Identity (IdP + SCIM)
              </h5>
              <p className="fs-10 mb-0 text-body-secondary">
                Configure per-tenant OIDC sign-in and provision the SCIM 2.0
                bearer token the IdP uses to push user lifecycle events.
                Changes apply to the <strong>currently selected tenant</strong>;
                switch tenants to configure a different one.
              </p>
            </Card.Body>
          </Card>
        </Col>
      </Row>

      <IdPConfigForm />
      <ScimTokenSection />
    </>
  );
};

export default IdentityAdminPage;
