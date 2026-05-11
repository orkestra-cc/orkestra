import { Col, Row } from 'react-bootstrap';
import AdvanceTableSearchBox from 'components/common/advance-table/AdvanceTableSearchBox';

const CompanyLookupTableHeader = () => {
  return (
    <div className="d-lg-flex justify-content-between">
      <Row className="flex-between-center gy-2 px-x1">
        <Col xs="auto" className="pe-0">
          <h6 className="mb-0">Aziende Cercate</h6>
        </Col>
        <Col xs="auto">
          <AdvanceTableSearchBox
            className="input-search-width"
            placeholder="Cerca per nome, P.IVA o codice fiscale"
          />
        </Col>
      </Row>
    </div>
  );
};

export default CompanyLookupTableHeader;
