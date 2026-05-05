import FalconCardHeader from 'components/common/FalconCardHeader';

import { Button, Card } from 'react-bootstrap';
import { Link } from 'react-router';

const DangerZone: React.FC = () => {
  return (
    <Card>
      <FalconCardHeader title="Danger Zone" />
      <Card.Body className="bg-body-tertiary">
        <h5 className="mb-0">Privacy & your data</h5>
        <p className="fs-10">
          Download a copy of the personal data we hold for your account, or
          exercise your GDPR right to erasure.
        </p>
        <Button
          as={Link as any}
          to="/user/privacy"
          variant="falcon-danger"
          className="w-100"
        >
          Manage privacy
        </Button>
      </Card.Body>
    </Card>
  );
};

export default DangerZone;
