import { Col, Row } from 'react-bootstrap';
import IssuedInvoiceGreetings from './IssuedInvoiceGreetings';
import IssuedInvoiceTable from './IssuedInvoiceTable';

const IssuedInvoicesPage: React.FC = () => {
  return (
    <>
      <Row className="g-3 mb-3">
        <Col xxl={12}>
          <IssuedInvoiceGreetings />
        </Col>
      </Row>
      <Row className="g-3">
        <Col xxl={12}>
          <IssuedInvoiceTable />
        </Col>
      </Row>
    </>
  );
};

export default IssuedInvoicesPage;
