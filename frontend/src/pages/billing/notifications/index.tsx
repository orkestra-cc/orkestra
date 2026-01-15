import { Col, Row } from 'react-bootstrap';
import NotificationGreetings from './NotificationGreetings';
import NotificationTable from './NotificationTable';

const SDINotificationsPage: React.FC = () => {
  return (
    <>
      <Row className="g-3 mb-3">
        <Col xxl={12}>
          <NotificationGreetings />
        </Col>
      </Row>
      <Row className="g-3">
        <Col xxl={12}>
          <NotificationTable />
        </Col>
      </Row>
    </>
  );
};

export default SDINotificationsPage;
