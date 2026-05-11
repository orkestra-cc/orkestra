import React from 'react';
import { Row, Col, Spinner, Alert } from 'react-bootstrap';
import WeeklySales from 'components/dashboards/default/WeeklySales';
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

// Import TanStack Query hooks
import {
  useDefaultDashboard,
  useBestSellingProducts
} from 'hooks/dashboard/useDashboardRTK';

/**
 * Default Dashboard with TanStack Query Integration
 * This is an example of how to migrate dashboard components to use TanStack Query
 *
 * Note: Currently using static fallback data as dashboard components
 * haven't been updated to accept isLoading/error props yet.
 */
const DefaultDashboardWithQuery: React.FC = () => {
  // Fetch multiple dashboard data sources
  const { data: dashboardData, isLoading, hasError, refetch } = useDefaultDashboard('30d');

  // Additional specific queries for other components
  const bestSellingProducts = useBestSellingProducts(8);

  if (isLoading) {
    return (
      <div className="d-flex justify-content-center align-items-center" style={{ minHeight: '400px' }}>
        <div className="text-center">
          <Spinner animation="border" className="mb-3" />
          <p className="text-muted">Loading dashboard data...</p>
        </div>
      </div>
    );
  }

  if (hasError) {
    return (
      <Alert variant="danger">
        <Alert.Heading>Error Loading Dashboard</Alert.Heading>
        <p>Failed to load dashboard data. Please try refreshing the page.</p>
        <div className="d-flex gap-2">
          <button
            className="btn btn-outline-danger btn-sm"
            onClick={() => refetch()}
          >
            Retry
          </button>
        </div>
      </Alert>
    );
  }

  return (
    <>
      <Row className="g-3 mb-3">
        <Col md={6} xxl={3}>
          <WeeklySales data={dashboardData?.weeklySales ?? weeklySalesData} />
        </Col>
        <Col md={6} xxl={3}>
          <TotalOrder data={dashboardData?.orders ?? totalOrder} />
        </Col>
        <Col md={6} xxl={3}>
          <MarketShare data={marketShare} radius={['100%', '87%']} />
        </Col>
        <Col md={6} xxl={3}>
          <Weather data={dashboardData?.weather ?? weather} />
        </Col>
      </Row>

      <Row className="g-3 mb-3">
        <Col lg={6}>
          <RunningProjects data={runningProjects} />
        </Col>
        <Col lg={6}>
          <TotalSales data={dashboardData?.sales ?? totalSales} />
        </Col>
      </Row>

      <Row className="g-3 mb-3">
        <Col lg={6} xl={7} xxl={8}>
          <StorageStatus
            className="h-lg-100"
            data={dashboardData?.storage ?? storageStatus}
          />
        </Col>
        <Col lg={6} xl={5} xxl={4}>
          <SpaceWarning />
        </Col>
      </Row>

      <Row className="g-3 mb-3">
        <Col lg={7} xl={8}>
          <BestSellingProducts products={bestSellingProducts.data ?? products} />
        </Col>
        <Col lg={5} xl={4}>
          <SharedFiles files={files} className="h-lg-100" />
        </Col>
      </Row>

      <Row className="g-3">
        <Col sm={6} xxl={3}>
          <ActiveUsers
            className="h-100"
            users={dashboardData?.activeUsers ?? users}
          />
        </Col>
        <Col sm={6} xxl={3} className="order-xxl-1">
          <BandwidthSaved />
        </Col>
        <Col xxl={6}>
          <TopProducts data={topProducts} />
        </Col>
      </Row>
    </>
  );
};

export default DefaultDashboardWithQuery;
