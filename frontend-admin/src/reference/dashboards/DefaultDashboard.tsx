
import WeeklySales from 'components/dashboards/default/WeeklySales';
import { Row, Col } from 'react-bootstrap';
import {
  marketShare,
  totalOrder,
  totalSales,
  weeklySalesData,
  weather,
  products,
  storageStatus,
  files,
  users,
  topProducts,
  runningProjects
} from 'data/dashboard/default';

import TotalOrder from 'components/dashboards/default/TotalOrder';
import MarketShare from 'components/dashboards/default/MarketShare';
import TotalSales from 'components/dashboards/default/TotalSales';
import RunningProjects from 'components/dashboards/default/RunningProjects';
import StorageStatus from 'components/dashboards/default/StorageStatus';
import SpaceWarning from 'components/dashboards/default/SpaceWarning';
import BestSellingProducts from 'components/dashboards/default/BestSellingProducts';
import SharedFiles from 'components/dashboards/default/SharedFiles';
import ActiveUsers from 'components/dashboards/default/ActiveUsers';
import BandwidthSaved from 'components/dashboards/default/BandwidthSaved';
import TopProducts from 'components/dashboards/default/TopProducts';
import Weather from 'components/dashboards/default/Weather';

const Dashboard: React.FC = () => {
  return (
    <>
      <Row className="g-3 mb-3">
        <Col md={6} xxl={3}>
          <WeeklySales data={weeklySalesData} />
        </Col>
        <Col md={6} xxl={3}>
          <TotalOrder data={totalOrder} />
        </Col>
        <Col md={6} xxl={3}>
          <MarketShare data={marketShare} radius={['100%', '87%']} />
        </Col>
        <Col md={6} xxl={3}>
          <Weather data={weather} />
        </Col>
      </Row>

      <Row className="g-3 mb-3">
        <Col lg={6}>
          <RunningProjects data={runningProjects} />
        </Col>
        <Col lg={6}>
          <TotalSales data={totalSales} />
        </Col>
      </Row>

      <Row className="g-3 mb-3">
        <Col lg={6} xl={7} xxl={8}>
          <StorageStatus className="h-lg-100" data={storageStatus} />
        </Col>
        <Col lg={6} xl={5} xxl={4}>
          <SpaceWarning />
        </Col>
      </Row>

      <Row className="g-3 mb-3">
        <Col lg={7} xl={8}>
          <BestSellingProducts products={products} />
        </Col>
        <Col lg={5} xl={4}>
          <SharedFiles files={files} className="h-lg-100" />
        </Col>
      </Row>

      <Row className="g-3">
        <Col sm={6} xxl={3}>
          <ActiveUsers className="h-100" users={users} />
        </Col>
        <Col sm={6} xxl={3} className="order-xxl-1">
          <BandwidthSaved />
        </Col>
        <Col xxl={6}>
          <TopProducts data={topProducts} className="h-100" />
        </Col>
      </Row>
    </>
  );
};

export default Dashboard;
