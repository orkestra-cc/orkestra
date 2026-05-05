import { Row, Col } from 'react-bootstrap';
import ModelsGreetings from './ModelsGreetings';
import ModelsTable from './ModelsTable';

const AIModelsPage = () => (
  <>
    <Row className="g-3 mb-3">
      <Col xxl={12}>
        <ModelsGreetings />
      </Col>
    </Row>
    <Row className="g-3">
      <Col xxl={12}>
        <ModelsTable />
      </Col>
    </Row>
  </>
);

export default AIModelsPage;
