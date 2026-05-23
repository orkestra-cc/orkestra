import { Card, Col, Row } from 'react-bootstrap';
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome';
import { Trans, useTranslation } from 'react-i18next';
import IdPConfigForm from './IdPConfigForm';
import ScimTokenSection from './ScimTokenSection';

// /identity is tenant-scoped — every call flows under the current
// X-Tenant-ID. Operators who want to configure a different tenant switch
// tenants via the tenant switcher first. Gated by tenant.update on the
// backend (see identity/module.go::RegisterRoutes).
const IdentityAdminPage: React.FC = () => {
  const { t } = useTranslation();
  return (
    <>
      <Row className="g-3 mb-3">
        <Col>
          <Card className="shadow-none border">
            <Card.Body>
              <h5 className="mb-1">
                <FontAwesomeIcon icon="id-card" className="me-2 text-primary" />
                {t('identityAddon.title')}
              </h5>
              <p className="fs-10 mb-0 text-body-secondary">
                <Trans
                  i18nKey="identityAddon.description"
                  components={{ strong: <strong /> }}
                />
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
