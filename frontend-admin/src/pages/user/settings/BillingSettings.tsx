import FalconCardHeader from 'components/common/FalconCardHeader';

import { Button, Card } from 'react-bootstrap';
import { Link } from 'react-router';

const BillingSettings: React.FC = () => {
  return (
    <Card className="mb-3">
      <FalconCardHeader title="Billing Setting" />
      <Card.Body className="bg-body-tertiary">
        <h5>Plan</h5>
        <p className="fs-9">
          <strong>Developer</strong> - Unlimited private repositories
        </p>
        <Button as={Link as any} variant="falcon-default" size="sm" to="#!">
          Update Plan
        </Button>
      </Card.Body>
      <Card.Body className="bg-body-tertiary border-top">
        <h5>Payment</h5>
        <p className="fs-9">You have not added any payment.</p>
        <Button as={Link as any} variant="falcon-default" size="sm" to="#!">
          Add Payment
        </Button>
      </Card.Body>
    </Card>
  );
};

export default BillingSettings;
