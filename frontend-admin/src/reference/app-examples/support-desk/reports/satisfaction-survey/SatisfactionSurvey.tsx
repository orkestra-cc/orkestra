
import CardDropdown from 'components/common/CardDropdown';
import OrkestraCardHeader from 'components/common/OrkestraCardHeader';
import SimpleBar from 'simplebar-react';
import { Card } from 'react-bootstrap';
import SatisfactionSurveyChart from './SatisfactionSurveyChart';
import OrkestraLink from 'components/common/OrkestraLink';

const SatisfactionSurvey = () => {
  return (
    <Card className="mt-3">
      <OrkestraCardHeader
        title="Customer Satisfaction Survey"
        titleTag="h6"
        className="py-2 border-bottom"
        endEl={<CardDropdown />}
      />
      <SimpleBar>
        <Card.Body>
          <SatisfactionSurveyChart />
        </Card.Body>
      </SimpleBar>
      <Card.Footer className="text-center bg-body-tertiary py-2">
        <OrkestraLink title="View all" className="px-0 fw-medium" />
      </Card.Footer>
    </Card>
  );
};

export default SatisfactionSurvey;
