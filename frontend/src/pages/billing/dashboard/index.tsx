import { Col, Row } from 'react-bootstrap';
import BillingGreetings from './BillingGreetings';
import BillingStatCards from './BillingStatCards';
import InvoiceTrendChart from './InvoiceTrendChart';
import RecentInvoices from './RecentInvoices';
import SDINotificationsSummary from './SDINotificationsSummary';
import PendingActions from './PendingActions';

const BillingDashboard: React.FC = () => {
  return (
    <>
      <Row className="g-3 mb-3">
        <Col xxl={12}>
          <BillingGreetings />
        </Col>
      </Row>
      <BillingStatCards />
      <Row className="g-3 mb-3">
        <Col lg={8}>
          <InvoiceTrendChart />
        </Col>
        <Col lg={4}>
          <SDINotificationsSummary />
        </Col>
      </Row>
      <Row className="g-3 mb-3">
        <Col lg={8}>
          <RecentInvoices />
        </Col>
        <Col lg={4}>
          <PendingActions />
        </Col>
      </Row>
    </>
  );
};

export default BillingDashboard;
