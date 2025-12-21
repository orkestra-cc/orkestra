
import CardDropdown from 'components/common/CardDropdown';
import FalconCardHeader from 'components/common/FalconCardHeader';
import { Button, Card, Col, Form, Row } from 'react-bootstrap';
// import SimpleBar from 'simplebar-react';
import DistributionOfPerformanceChart from './DistributionOfPerformanceChart';
import { Link } from 'react-router';

const DistributionOfPerformance = () => {
  return (
    <Card className="mt-3">
      <FalconCardHeader
        title="Distribution of Performance"
        titleTag="h6"
        className="py-2 border-bottom"
        endEl={<CardDropdown />}
      />
      <Card.Body>
        <DistributionOfPerformanceChart />
      </Card.Body>
      <Card.Footer className="bg-body-tertiary py-2">
        <Row className="flex-between-center">
          <Col xs="auto">
            <Form.Select defaultValue="November" size="sm">
              <option>January</option>
              <option>February</option>
              <option>March</option>
              <option>April</option>
              <option>May</option>
              <option>Jun</option>
              <option>July</option>
              <option>September</option>
              <option>October</option>
              <option>November</option>
              <option>December</option>
            </Form.Select>
          </Col>
          <Col xs="auto">
            <Button variant="falcon-default" size="sm" as={Link as any} to="#!">
              View All
            </Button>
          </Col>
        </Row>
      </Card.Footer>
    </Card>
  );
};

export default DistributionOfPerformance;
