import { Col, Row, Alert } from 'react-bootstrap';
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome';
import PageHeader from 'components/common/PageHeader';
import CompanyTable from './CompanyTable';

const CompanyManagementPage: React.FC = () => {
  return (
    <>
      <PageHeader
        title="Aziende Emittenti"
        description="Gestisci le aziende per l'emissione delle fatture elettroniche"
        className="mb-3"
      />
      <Row className="g-3 mb-3">
        <Col xxl={12}>
          <Alert variant="info" className="d-flex align-items-center">
            <FontAwesomeIcon icon="info-circle" className="me-2" />
            <div>
              <strong>Configurazione Aziende:</strong> Le aziende qui configurate verranno utilizzate
              come cedente/prestatore (venditore) nelle fatture elettroniche.
              L'azienda impostata come <em>Default</em> verrà pre-selezionata automaticamente
              nella creazione di nuove fatture.
            </div>
          </Alert>
        </Col>
      </Row>
      <Row className="g-3">
        <Col xxl={12}>
          <CompanyTable />
        </Col>
      </Row>
    </>
  );
};

export default CompanyManagementPage;
