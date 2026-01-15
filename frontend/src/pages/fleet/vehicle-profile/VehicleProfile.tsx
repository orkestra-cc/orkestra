
import { useParams } from 'react-router';
import { useGetVehicleByIdQuery } from 'store/api/vehicleApi';
import VehicleBanner from './VehicleBanner';
import VehicleProfileInfo from './VehicleProfileInfo';
import { Col, Row, Alert, Spinner } from 'react-bootstrap';
import VehicleMaintenanceLog from './VehicleMaintenanceLog';
import VehicleActions from './VehicleActions';
import VehicleStats from './VehicleStats';

const VehicleProfile: React.FC = () => {
  const { vehicleId } = useParams<{ vehicleId: string }>();

  const {
    data: vehicle,
    isLoading,
    error
  } = useGetVehicleByIdQuery(vehicleId!, {
    skip: !vehicleId
  });

  if (isLoading) {
    return (
      <div className="d-flex justify-content-center align-items-center" style={{ minHeight: '400px' }}>
        <Spinner animation="border" role="status">
          <span className="visually-hidden">Loading...</span>
        </Spinner>
      </div>
    );
  }

  if (error) {
    return (
      <Alert variant="danger">
        Error loading vehicle data. Please try again later.
      </Alert>
    );
  }

  if (!vehicle) {
    return (
      <Alert variant="warning">
        Vehicle not found.
      </Alert>
    );
  }

  return (
    <>
      <VehicleBanner vehicle={vehicle} />
      <Row className="g-3 mb-3">
        <Col lg={8}>
          <VehicleProfileInfo vehicle={vehicle} />
          <VehicleMaintenanceLog className="mt-3" vehicleId={vehicleId!} />
        </Col>
        <Col lg={4}>
          <div className="sticky-sidebar">
            <VehicleActions vehicle={vehicle} />
            <VehicleStats vehicle={vehicle} />
          </div>
        </Col>
      </Row>
    </>
  );
};

export default VehicleProfile;