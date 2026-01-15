
import { Col, Row } from 'react-bootstrap';
import VehicleGreetings from 'pages/fleet/vehicles/VehicleGreetings';
import VehicleTable from 'pages/fleet/vehicles/VehicleTable';

const VehicleManagementDashboard: React.FC = () => {
  return (
    <>
      <Row className="g-3 mb-3">
        <Col xxl={12}>
          <VehicleGreetings />
        </Col>
      </Row>
      <Row className="g-3">
        <Col xxl={12}>
          <VehicleTable />
        </Col>
      </Row>
    </>
  );
};

export default VehicleManagementDashboard;