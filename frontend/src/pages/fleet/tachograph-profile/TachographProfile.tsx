
import { useParams } from 'react-router';
import { useGetTachographByIdQuery } from 'store/api/tachographApi';
import TachographBanner from './TachographBanner';
import TachographProfileInfo from './TachographProfileInfo';
import { Col, Row, Alert, Spinner } from 'react-bootstrap';
import TachographActions from './TachographActions';

const TachographProfile: React.FC = () => {
  const { tachographId } = useParams<{ tachographId: string }>();

  const {
    data: tachograph,
    isLoading,
    error
  } = useGetTachographByIdQuery(tachographId!, {
    skip: !tachographId
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
        Error loading tachograph data. Please try again later.
      </Alert>
    );
  }

  if (!tachograph) {
    return (
      <Alert variant="warning">
        Tachograph not found.
      </Alert>
    );
  }

  return (
    <>
      <TachographBanner tachograph={tachograph} />
      <Row className="g-3 mb-3">
        <Col lg={8}>
          <TachographProfileInfo tachograph={tachograph} />
        </Col>
        <Col lg={4}>
          <div className="sticky-sidebar">
            <TachographActions tachograph={tachograph} />
          </div>
        </Col>
      </Row>
    </>
  );
};

export default TachographProfile;