import { Col, Row } from 'react-bootstrap';
import CompanyLookupGreetings from './CompanyLookupGreetings';
import CompanyLookupSearch from './CompanyLookupSearch';
import CompanyLookupTable from './CompanyLookupTable';

const CompanyLookupPage: React.FC = () => {
  return (
    <>
      <Row className="g-3 mb-3">
        <Col xxl={12}>
          <CompanyLookupGreetings />
        </Col>
      </Row>
      <Row className="g-3 mb-3">
        <Col xxl={12}>
          <CompanyLookupSearch />
        </Col>
      </Row>
      <Row className="g-3">
        <Col xxl={12}>
          <CompanyLookupTable />
        </Col>
      </Row>
    </>
  );
};

export default CompanyLookupPage;
