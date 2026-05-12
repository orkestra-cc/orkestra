import { Card } from 'react-bootstrap';
import OrkestraCardHeader from 'components/common/OrkestraCardHeader';
import DealForeCastTable from './DealForeCastTable';
import { dealForecastByOwnerData } from 'data/dashboard/crm';

const DealForeCast = () => {
  return (
    <Card className="overflow-hidden">
      <OrkestraCardHeader title="Deal Forecast by Owner" titleTag="h6" />
      <Card.Body className="p-0">
        <DealForeCastTable data={dealForecastByOwnerData} />
      </Card.Body>
    </Card>
  );
};

export default DealForeCast;
