
import { Col, Row } from 'react-bootstrap';
import CraneGreetings from 'pages/fleet/cranes/CraneGreetings';
import CraneTable from 'pages/fleet/cranes/CraneTable';

const CraneManagementDashboard: React.FC = () => {
  return (
    <>
      <Row className="g-3 mb-3">
        <Col xxl={12}>
          <CraneGreetings />
        </Col>
      </Row>
      <Row className="g-3">
        <Col xxl={12}>
          <CraneTable />
        </Col>
      </Row>
    </>
  );
};

export default CraneManagementDashboard;