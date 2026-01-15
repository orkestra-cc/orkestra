import FalconLink from 'components/common/FalconLink';
import { bounceRate } from 'data/dashboard/analytics';

import { Card, Col, Form, Row } from 'react-bootstrap';
import BounceRateChart from './BounceRateChart';

// TypeScript interfaces
interface BounceRateProps extends React.ComponentProps<typeof Card> {
  // Additional props if needed
}

const BounceRate: React.FC<BounceRateProps> = ({ ...rest }) => {
  return (
    <Card {...rest}>
      <Card.Header>
        <h5 className="text-900 fs-9 mb-2">Trend of Bounce Rate</h5>
        <h6 className="mb-0 fs-11 text-500">Nov 1, 2020–Jan 31, 2021</h6>
      </Card.Header>
      <Card.Body>
        <BounceRateChart data={bounceRate} />
      </Card.Body>
      <Card.Footer className="bg-body-tertiary py-2">
        <Row className="g-0 flex-between-center">
          <Col xs="auto">
            <Form.Select size="sm" className="me-2">
              <option>Last Month</option>
              <option>Last Year</option>
            </Form.Select>
          </Col>
          <Col xs="auto">
            <FalconLink title="View full report" className="px-0 fw-medium" />
          </Col>
        </Row>
      </Card.Footer>
    </Card>
  );
};

export default BounceRate;
