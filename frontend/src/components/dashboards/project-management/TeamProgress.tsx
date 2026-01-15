import FalconCardHeader from 'components/common/FalconCardHeader';
import FalconLink from 'components/common/FalconLink';

import { Card, ProgressBar } from 'react-bootstrap';

const TeamProgress: React.FC = () => {
  return (
    <Card className="h-100">
      <FalconCardHeader
        title="Team Progress"
        endEl={<FalconLink title="Report" className="px-0" />}
      />
      <Card.Body>
        <p className="fs-10 text-600">
          See team members' time worked, <br /> activity levels, and progress
        </p>

        <ProgressBar
          now={75}
          key={1}
          style={{ height: '6px' }}
          className="rounded-pill mb-3"
        />

        <p className="mb-0 text-primary"> 75% completed</p>
        <p className="mb-0 fs-11 text-500">Jan 1st to 30th</p>
      </Card.Body>
    </Card>
  );
};

export default TeamProgress;
