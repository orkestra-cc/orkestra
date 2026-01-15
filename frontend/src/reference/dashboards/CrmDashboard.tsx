
import { Col, Row } from 'react-bootstrap';
import CrmStats from 'components/dashboards/crm/CrmStats';
import DealForecastBar from 'components/dashboards/crm/DealForecastBar';
import DealStorageFunnel from 'components/dashboards/crm/deal-storage-funnel/DealStorageFunnel';
import MostLeads from 'components/dashboards/crm/most-leads/MostLeads';
import Revenue from 'components/dashboards/crm/revenue/Revenue';
import DealVsGoal from 'components/dashboards/crm/deal-vs-goal/DealVsGoal';
import DealForeCast from 'components/dashboards/crm/deal-forecast/DealForeCast';
import LocationBySession from 'components/dashboards/crm/LocationBySession/LocationBySession';
import AvgCallDuration from 'components/dashboards/crm/avg-call-duration/AvgCallDuration';
import LeadConversation from 'components/dashboards/crm/lead-conversation/LeadConversation';
import ToDoList from 'components/dashboards/crm/ToDoList';
import RecentLeads from 'components/dashboards/crm/recent-leads/RecentLeads';
import Greetings from 'components/dashboards/crm/greetings/Greetings';

const Crm: React.FC = () => {
  return (
    <>
      <Greetings />
      <Row className="g-3 mb-3">
        <Col xxl={9}>
          <CrmStats />
          <Revenue />
        </Col>
        <Col xxl={3}>
          <MostLeads />
        </Col>
        <Col md={12} xxl={8}>
          <DealForecastBar />
        </Col>
        <Col xxl={4}>
          <DealStorageFunnel />
        </Col>
        <Col xxl={6}>
          <DealVsGoal />
        </Col>
        <Col xxl={6}>
          <DealForeCast />
        </Col>
      </Row>
      <Row className="g-3 mb-3">
        <Col lg={7}>
          <LocationBySession />
        </Col>
        <Col lg={5}>
          <Row className="g-3">
            <Col xs={12}>
              <AvgCallDuration />
            </Col>
            <Col xs={12}>
              <LeadConversation />
            </Col>
          </Row>
        </Col>
      </Row>
      <Row className="g-3">
        <Col lg={5}>
          <ToDoList />
        </Col>
        <Col lg={7}>
          <RecentLeads />
        </Col>
      </Row>
    </>
  );
};

export default Crm;
