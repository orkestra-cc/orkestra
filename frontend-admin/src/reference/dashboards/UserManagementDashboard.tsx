
import { Col, Row } from 'react-bootstrap';
import UserGreetings from 'pages/admin/users/UserGreetings';
import UserTable from 'pages/admin/users/UserTable';

const UserManagementDashboard: React.FC = () => {
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

export default UserManagementDashboard;