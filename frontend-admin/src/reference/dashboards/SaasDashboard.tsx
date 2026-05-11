
import { Col, Row } from 'react-bootstrap';
import LinePayment from 'components/dashboards/saas/line-payment/LinePayment';
import {
  payment,
  activeUser,
  transactionSummary,
  grossRevenue,
  candleChartData
} from 'data/dashboard/saas';
import SaasActiveUser from 'components/dashboards/saas/SaasActiveUser';
import SaasRevenue from 'components/dashboards/saas/SaasRevenue';
import SaasConversion from 'components/dashboards/saas/SaasConversion';
import DepositeStatus from 'components/dashboards/saas/DepositeStatus';
import StatisticsCards from 'components/dashboards/saas/stats-cards/StatisticsCards';
import { users, files } from 'data/dashboard/default';
import ActiveUsers from 'components/dashboards/default/ActiveUsers';
import SharedFiles from 'components/dashboards/default/SharedFiles';
import BandwidthSaved from 'components/dashboards/default/BandwidthSaved';
import DoMoreCard from 'components/dashboards/saas/DoMoreCard';
import TransactionSummary from 'components/dashboards/saas/TransactionSummary';
import GrossRevenue from 'components/dashboards/saas/gross-revenue/GrossRevenue';
import CandleChart from 'components/dashboards/saas/candle-chart/CandleChart';

const Saas: React.FC = () => {
  return (
    <>
      <Row className="g-3">
        <Col xxl={9}>
          <LinePayment data={payment} />
        </Col>
        <Col>
          <Row className="g-3">
            <Col md={4} xxl={12}>
              <SaasActiveUser data={activeUser} />
            </Col>
            <Col md={4} xxl={12}>
              <SaasRevenue />
            </Col>
            <Col md={4} xxl={12}>
              <SaasConversion />
            </Col>
          </Row>
        </Col>
      </Row>
      <Row className="g-3 mb-3">
        <Col xxl={9}>
          <DepositeStatus />
          <StatisticsCards />
          <Row className="g-3">
            <Col xs={12}>
              <CandleChart data={candleChartData} />
            </Col>
            <Col lg={4}>
              <ActiveUsers users={users} end={7} />
            </Col>
            <Col lg={8}>
              <GrossRevenue data={grossRevenue} />
            </Col>
          </Row>
        </Col>
        <Col xxl={3}>
          <Row className="g-3">
            <Col xxl={12}>
              <SharedFiles
                files={files}
                className="h-100 h-xxl-auto mt-xxl-3"
              />
            </Col>
            <Col md={6} xxl={12}>
              <BandwidthSaved />
            </Col>
            <Col md={6} xxl={12}>
              <DoMoreCard />
            </Col>
          </Row>
        </Col>
      </Row>
      <TransactionSummary data={transactionSummary} />
    </>
  );
};

export default Saas;
