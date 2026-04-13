import { Col, Row } from 'react-bootstrap';
import TemplatesGreetings from './TemplatesGreetings';
import TemplatesTable from './TemplatesTable';

const TemplatesPage: React.FC = () => {
  return (
    <>
      <Row className="g-3 mb-3">
        <Col xxl={12}>
          <TemplatesGreetings />
        </Col>
      </Row>
      <Row className="g-3">
        <Col xxl={12}>
          <TemplatesTable />
        </Col>
      </Row>
    </>
  );
};

export default TemplatesPage;
