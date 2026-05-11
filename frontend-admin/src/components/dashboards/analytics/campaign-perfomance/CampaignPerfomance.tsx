
import { Card, Col, Form, Row } from 'react-bootstrap';
import FalconCardHeader from 'components/common/FalconCardHeader';
import CardDropdown from 'components/common/CardDropdown';
import FalconLink from 'components/common/FalconLink';
import PerfomanceTable from './PerfomanceTable';
import { campaignTable, campaignChart } from 'data/dashboard/analytics';
import CampaignChart from './CampaignChart';
import { useAppContext } from 'providers/AppProvider';

const CampaignPerfomance = () => {
  const { getThemeColor } = useAppContext();
  return (
    <Card className="h-100">
      <FalconCardHeader
        title="Ad campaigns perfomance"
        titleTag="h6"
        className="py-2"
        light
        endEl={<CardDropdown />}
      />
      <Card.Body className="pb-0">
        <Row>
          <Col xs={6}>
            <h6 className="text-700">Revenue</h6>
            <h3 className="fw-normal text-700">$10.87k</h3>
            <CampaignChart
              color={getThemeColor('warning')}
              data={campaignChart.revenue}
            />
          </Col>
          <Col xs={6}>
            <h6 className="text-700">Clicks</h6>
            <h3 className="fw-normal text-700">3.8k</h3>
            <CampaignChart
              color={getThemeColor('primary')}
              data={campaignChart.clicks}
            />
          </Col>
        </Row>
        <div className="mx-ncard">
          <PerfomanceTable data={campaignTable} />
        </div>
      </Card.Body>

      <Card.Footer className="bg-body-tertiary py-2">
        <Row className="g-0 flex-between-center">
          <Col xs="auto">
            <Form.Select size="sm" className="me-2" name="date-range" aria-label="Date range">
              <option>Last 7 days</option>
              <option>Last Month</option>
              <option>Last Year</option>
            </Form.Select>
          </Col>
          <Col xs="auto">
            <FalconLink title="Browser Overview" className="px-0 fw-medium" />
          </Col>
        </Row>
      </Card.Footer>
    </Card>
  );
};

export default CampaignPerfomance;
