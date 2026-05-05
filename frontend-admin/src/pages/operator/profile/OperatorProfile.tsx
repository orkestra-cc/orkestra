
import { useGetUserByIdQuery } from 'store/api/userApi';
import { useSelector } from 'react-redux';
import { selectUser } from 'store/slices/authSlice';
import OperatorBanner from './OperatorBanner';
import OperatorProfileIntro from './OperatorProfileIntro';
import { Col, Row, Alert, Spinner } from 'react-bootstrap';


const OperatorProfile: React.FC = () => {
  const currentUser = useSelector(selectUser);
  const userId = currentUser?.id;

  const {
    data: user,
    isLoading,
    error
  } = useGetUserByIdQuery(userId!, {
    skip: !userId
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
        Error loading user data. Please try again later.
      </Alert>
    );
  }

  if (!user) {
    return (
      <Alert variant="warning">
        User not found.
      </Alert>
    );
  }

  return (
    <>
      <OperatorBanner user={user} />
      <Row className="g-3 mb-3">
        <Col lg={12}>
          <OperatorProfileIntro user={user} />

        </Col>
      </Row>
    </>
  );
};

export default OperatorProfile;
