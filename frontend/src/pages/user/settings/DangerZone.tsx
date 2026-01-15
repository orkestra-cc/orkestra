import FalconCardHeader from 'components/common/FalconCardHeader';

import { Button, Card } from 'react-bootstrap';
import { Link } from 'react-router';

const DangerZone: React.FC = () => {
  return (
    <Card>
      <FalconCardHeader title="Danger Zone" />
      <Card.Body className="bg-body-tertiary">
        <h5 className="mb-0">Transfer Ownership</h5>
        <p className="fs-10">
          Transfer this account to another user or to an organization where you
          have the ability to create repositories.
        </p>
        <Button as={Link as any} to="#!" variant="falcon-warning" className="w-100">
          Transfer
        </Button>
        <div className="border-dashed border-bottom my-4" />
        <h5 className="mb-0">Delete this account</h5>
        <p className="fs-10">
          Once you delete a account, there is no going back. Please be certain.
        </p>
        <Button as={Link as any} to="#!" variant="falcon-danger" className="w-100">
          Deactivate Account
        </Button>
      </Card.Body>
    </Card>
  );
};

export default DangerZone;
