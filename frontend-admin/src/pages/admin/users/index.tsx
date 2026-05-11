import { Col, Row } from 'react-bootstrap';
import UserGreetings from './UserGreetings';
import UserTable from './UserTable';

const UserManagementPage: React.FC = () => {
  return (
    <>
      <Row className="g-3 mb-3">
        <Col xxl={12}>
          <UserGreetings />
        </Col>
      </Row>
      <Row className="g-3">
        <Col xxl={12}>
          <UserTable />
        </Col>
      </Row>
    </>
  );
};

export default UserManagementPage;
