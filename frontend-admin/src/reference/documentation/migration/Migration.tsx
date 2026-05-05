
import MigrationSidebar from './MigrationSidebar';
import { Row, Col } from 'react-bootstrap';
import MigrationToVite from './MigrationToVite';
import MigrationToNineteen from './MigrationToNineteen';



const Migration = () => (
  <Row className="g-3">
    <Col xs={12} lg={4} xl={3} className="order-lg-1">
      <MigrationSidebar />
    </Col>

    <Col xs={12} lg={8} xl={9}>
      <MigrationToNineteen />
      <MigrationToVite />
    </Col>
  </Row>
);
export default Migration;
