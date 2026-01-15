
import { useParams } from 'react-router';
import { useGetCraneByIdQuery } from 'store/api/craneApi';
import CraneBanner from './CraneBanner';
import CraneProfileInfo from './CraneProfileInfo';
import { Col, Row, Alert, Spinner } from 'react-bootstrap';
import CraneVerificationLog from './CraneVerificationLog';
import CraneActions from './CraneActions';
import CraneStats from './CraneStats';

const CraneProfile: React.FC = () => {
  const { craneId } = useParams<{ craneId: string }>();

  const {
    data: crane,
    isLoading,
    error
  } = useGetCraneByIdQuery(craneId!, {
    skip: !craneId
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
        Error loading crane data. Please try again later.
      </Alert>
    );
  }

  if (!crane) {
    return (
      <Alert variant="warning">
        Crane not found.
      </Alert>
    );
  }

  return (
    <>
      <CraneBanner crane={crane} />
      <Row className="g-3 mb-3">
        <Col lg={8}>
          <CraneProfileInfo crane={crane} />
          <CraneVerificationLog className="mt-3" craneId={craneId!} />
        </Col>
        <Col lg={4}>
          <div className="sticky-sidebar">
            <CraneActions crane={crane} />
            <CraneStats crane={crane} />
          </div>
        </Col>
      </Row>
    </>
  );
};

export default CraneProfile;