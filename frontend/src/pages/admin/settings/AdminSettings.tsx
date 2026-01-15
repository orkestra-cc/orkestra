import { Card, Col, Row } from 'react-bootstrap';
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome';
import PageHeader from 'components/common/PageHeader';

const AdminSettings = () => {
  return (
    <>
      <PageHeader
        title="Settings"
        description="System configuration and preferences"
        className="mb-3"
      />
      <Row className="g-3">
        <Col lg={12}>
          <Card>
            <Card.Body className="text-center py-5">
              <FontAwesomeIcon
                icon="cog"
                className="text-400 mb-3"
                style={{ fontSize: '3rem' }}
              />
              <h4 className="text-700">Settings Coming Soon</h4>
              <p className="text-500 mb-0">
                System settings and configuration options will be available here.
              </p>
            </Card.Body>
          </Card>
        </Col>
      </Row>
    </>
  );
};

export default AdminSettings;
