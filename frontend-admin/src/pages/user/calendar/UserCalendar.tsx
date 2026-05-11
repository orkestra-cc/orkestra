import { Card, Col, Row } from 'react-bootstrap';
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome';
import PageHeader from 'components/common/PageHeader';

const UserCalendar = () => {
  return (
    <>
      <PageHeader
        title="Calendar"
        description="View and manage your schedule"
        className="mb-3"
      />
      <Row className="g-3">
        <Col lg={12}>
          <Card>
            <Card.Body className="text-center py-5">
              <FontAwesomeIcon
                icon="calendar-alt"
                className="text-400 mb-3"
                style={{ fontSize: '3rem' }}
              />
              <h4 className="text-700">Calendar Coming Soon</h4>
              <p className="text-500 mb-0">
                Your personal calendar with schedules, events, and reminders will be available here.
              </p>
            </Card.Body>
          </Card>
        </Col>
      </Row>
    </>
  );
};

export default UserCalendar;
