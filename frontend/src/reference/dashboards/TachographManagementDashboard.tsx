
import { Col, Row } from 'react-bootstrap';
import TachographGreetings from 'pages/fleet/tachographs/TachographGreetings';
import TachographTable from 'pages/fleet/tachographs/TachographTable';

const TachographManagementDashboard: React.FC = () => {
  return (
    <>
      <Row className="g-3 mb-3">
        <Col xxl={12}>
          <TachographGreetings />
        </Col>
      </Row>
      <Row className="g-3">
        <Col xxl={12}>
          <TachographTable />
        </Col>
      </Row>
    </>
  );
};

export default TachographManagementDashboard;