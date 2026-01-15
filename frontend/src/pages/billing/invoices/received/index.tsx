import { Col, Row } from 'react-bootstrap';
import ReceivedInvoiceGreetings from './ReceivedInvoiceGreetings';
import ReceivedInvoiceTable from './ReceivedInvoiceTable';

const ReceivedInvoicesPage: React.FC = () => {
  return (
    <>
      <Row className="g-3 mb-3">
        <Col xxl={12}>
          <ReceivedInvoiceGreetings />
        </Col>
      </Row>
      <Row className="g-3">
        <Col xxl={12}>
          <ReceivedInvoiceTable />
        </Col>
      </Row>
    </>
  );
};

export default ReceivedInvoicesPage;
