import { Card, Col, Row } from 'react-bootstrap';
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome';
import PageHeader from 'components/common/PageHeader';

const UserDashboard = () => {
  return (
    <>
      <PageHeader
        title="Dashboard"
        description="Your personal overview and activity summary"
        className="mb-3"
      />
      <Row className="g-3">
        <Col lg={12}>
          <Card>
            <Card.Body className="text-center py-5">
              <FontAwesomeIcon
                icon="chart-pie"
                className="text-400 mb-3"
                style={{ fontSize: '3rem' }}
              />
              <h4 className="text-700">Dashboard Coming Soon</h4>
              <p className="text-500 mb-0">
                Your personal dashboard with activity summaries and quick actions will be available here.
              </p>
            </Card.Body>
          </Card>
        </Col>
      </Row>
    </>
  );
};

export default UserDashboard;
