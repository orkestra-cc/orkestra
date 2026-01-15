import { Col, Row } from 'react-bootstrap';
import CustomerGreetings from './CustomerGreetings';
import CustomerTable from './CustomerTable';

const CustomerManagementPage: React.FC = () => {
  return (
    <>
      <Row className="g-3 mb-3">
        <Col xxl={12}>
          <CustomerGreetings />
        </Col>
      </Row>
      <Row className="g-3">
        <Col xxl={12}>
          <CustomerTable />
        </Col>
      </Row>
    </>
  );
};

export default CustomerManagementPage;
