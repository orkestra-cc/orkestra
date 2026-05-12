import { Card, Col, Form, Row } from 'react-bootstrap';
import OrkestraCardHeader from 'components/common/OrkestraCardHeader';
import CardDropdown from 'components/common/CardDropdown';
import OrkestraLink from 'components/common/OrkestraLink';
import TrafficSourceChart from './TrafficSourceChart';

function TrafficSource() {
  return (
    <Card className="h-100">
      <OrkestraCardHeader
        title="Traffic source"
        titleTag="h6"
        className="py-2"
        light
        endEl={<CardDropdown />}
      />
      <Card.Body>
        <TrafficSourceChart />
      </Card.Body>

      <Card.Footer className="bg-body-tertiary py-2">
        <Row className="g-0 flex-between-center">
          <Col xs="auto">
            <Form.Select size="sm" name="date-range" aria-label="Date range">
              <option>Last 7 days</option>
              <option>Last Month</option>
              <option>Last Year</option>
            </Form.Select>
          </Col>
          <Col xs="auto">
            <OrkestraLink
              title="Acquisition overview"
              className="px-0 fw-medium"
            />
          </Col>
        </Row>
      </Card.Footer>
    </Card>
  );
}

export default TrafficSource;
