
import { useParams } from 'react-router';
import { useGetUserByIdQuery } from 'store/api/userApi';
import AdminBanner from './AdminBanner';
import AdminProfileIntro from './AdminProfileIntro';
import { Col, Row, Alert, Spinner } from 'react-bootstrap';
import AdminActivityLog from './AdminActivityLog';
import AdminUserActions from './AdminUserActions';
import AdminUserMetrics from './AdminUserMetrics';
import AdminLicenses from './AdminLicenses';
import AdminMedicalChecks from './AdminMedicalChecks';

const AdminUserProfile: React.FC = () => {
  const { userId } = useParams<{ userId: string }>();

  const {
    data: user,
    isLoading,
    error
  } = useGetUserByIdQuery(userId!, {
    skip: !userId
  });

  if (isLoading) {
    return (
      <div
        className="d-flex justify-content-center align-items-center"
        style={{ minHeight: '400px' }}
      >
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
    return <Alert variant="warning">User not found.</Alert>;
  }

  return (
    <>
      <AdminBanner user={user} />
      <Row className="g-3 mb-3">
        <Col lg={8}>
          <AdminProfileIntro user={user} />
          <AdminLicenses user={user} />
          <AdminMedicalChecks user={user} />
          <AdminActivityLog className="mt-3" userId={userId!} />
        </Col>
        <Col lg={4}>
          <div className="sticky-sidebar">
            <AdminUserActions user={user} />
            <AdminUserMetrics userId={userId!} />
          </div>
        </Col>
      </Row>
    </>
  );
};

export default AdminUserProfile;
