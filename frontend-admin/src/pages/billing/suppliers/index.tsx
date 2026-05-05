import { Col, Row } from 'react-bootstrap';
import SupplierGreetings from './SupplierGreetings';
import SupplierTable from './SupplierTable';

const SupplierManagementPage: React.FC = () => {
  return (
    <>
      <Row className="g-3 mb-3">
        <Col xxl={12}>
          <SupplierGreetings />
        </Col>
      </Row>
      <Row className="g-3">
        <Col xxl={12}>
          <SupplierTable />
        </Col>
      </Row>
    </>
  );
};

export default SupplierManagementPage;
