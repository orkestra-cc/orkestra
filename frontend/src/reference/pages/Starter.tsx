
import { Button, Card, Col, Row } from 'react-bootstrap';
import { Link } from 'react-router';
import editing from 'assets/img/icons/spot-illustrations/21.png';
import paths from 'routes/paths';

const Starter: React.FC = () => {
  return (
    <Card>
      <Card.Body className="overflow-hidden p-lg-6">
        <Row className="align-items-center justify-content-between">
          <Col lg={6}>
            <img src={editing} className="img-fluid" alt="" />
          </Col>
          <Col lg={6} className="ps-lg-4 my-5 text-center text-lg-start">
            <h3 className="text-primary">Edit me!</h3>
            <p className="lead">Create Something Beautiful.</p>
            <Button
              variant="falcon-primary"
              as={Link as any}
              to={paths.gettingStarted}
            >
              Getting started
            </Button>
          </Col>
        </Row>
      </Card.Body>
    </Card>
  );
};

export default Starter;
