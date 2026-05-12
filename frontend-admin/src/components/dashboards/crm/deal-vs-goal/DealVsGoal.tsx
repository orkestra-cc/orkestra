import { Card } from 'react-bootstrap';
import OrkestraCardHeader from 'components/common/OrkestraCardHeader';
import CardDropdown from 'components/common/CardDropdown';
import DealVSGoalChart from './DealVsGoalChart';
import { dealClosedVsGoalChart } from 'data/dashboard/crm';

const DealVsGoal = () => {
  return (
    <Card className="h-100">
      <OrkestraCardHeader
        title="Deal Closed vs Goal"
        titleTag="h6"
        className="py-2"
        endEl={<CardDropdown />}
      />
      <Card.Body>
        <DealVSGoalChart data={dealClosedVsGoalChart} />
      </Card.Body>
    </Card>
  );
};

export default DealVsGoal;
