import { Card, ProgressBar, Row, Col, Alert, Spinner } from 'react-bootstrap';
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome';
import { useTranslation } from 'react-i18next';
import Flex from 'components/common/Flex';
import { useGetUserMetricsQuery } from 'store/api/userApi';

interface AdminUserMetricsProps {
  userId: string;
}

const AdminUserMetrics: React.FC<AdminUserMetricsProps> = ({ userId }) => {
  const { t } = useTranslation();
  const {
    data: metricsData,
    isLoading,
    error
  } = useGetUserMetricsQuery(userId);
  // If loading, show spinner
  if (isLoading) {
    return (
      <div
        className="d-flex justify-content-center align-items-center"
        style={{ minHeight: '200px' }}
      >
        <Spinner animation="border" role="status">
          <span className="visually-hidden">
            {t('adminUserProfile.loadingMetricsAria')}
          </span>
        </Spinner>
      </div>
    );
  }

  // If error, show error message
  if (error) {
    return (
      <Alert variant="danger">
        {t('adminUserProfile.metrics.errorNoData')}
      </Alert>
    );
  }

  // If no data, use default values
  if (!metricsData) {
    return (
      <Alert variant="warning">
        {t('adminUserProfile.metrics.noMetricsAvailable')}
      </Alert>
    );
  }

  const metrics = [
    {
      icon: 'calendar-check',
      label: t('adminUserProfile.metrics.tasksCompleted'),
      value: `${metricsData.tasksCompleted}`,
      total: `${metricsData.totalTasks}`,
      percentage: Math.round(
        (metricsData.tasksCompleted / metricsData.totalTasks) * 100
      ),
      variant: 'success'
    },
    {
      icon: 'clock',
      label: t('adminUserProfile.metrics.onTimeDeliveries'),
      value: t('adminUserProfile.metrics.percent', {
        value: Math.round(metricsData.onTimeDeliveryRate)
      }),
      percentage: Math.round(metricsData.onTimeDeliveryRate),
      variant: 'info'
    },
    {
      icon: 'star',
      label: t('adminUserProfile.metrics.performanceRating'),
      value: t('adminUserProfile.metrics.ratingValue', {
        value: metricsData.performanceRating.toFixed(1)
      }),
      percentage: Math.round((metricsData.performanceRating / 5) * 100),
      variant: 'warning'
    },
    {
      icon: 'users',
      label: t('adminUserProfile.metrics.teamCollaboration'),
      value: t('adminUserProfile.metrics.percent', {
        value: Math.round(metricsData.teamCollaboration)
      }),
      percentage: Math.round(metricsData.teamCollaboration),
      variant: 'primary'
    }
  ];

  const systemUsage = [
    {
      feature: t('adminUserProfile.metrics.featureDashboard'),
      usage: metricsData.systemUsage.dashboard,
      color: 'primary'
    },
    {
      feature: t('adminUserProfile.metrics.featureReports'),
      usage: metricsData.systemUsage.reports,
      color: 'success'
    },
    {
      feature: t('adminUserProfile.metrics.featureSettings'),
      usage: metricsData.systemUsage.settings,
      color: 'warning'
    },
    {
      feature: t('adminUserProfile.metrics.featureHelpDesk'),
      usage: metricsData.systemUsage.helpDesk,
      color: 'secondary'
    }
  ];

  return (
    <>
      <Card className="mb-3">
        <Card.Header className="bg-body-tertiary">
          <h5 className="mb-0">
            <FontAwesomeIcon icon="chart-bar" className="me-2" />
            {t('adminUserProfile.metrics.performanceTitle')}
          </h5>
        </Card.Header>
        <Card.Body>
          {metrics.map((metric, index) => (
            <div
              key={index}
              className={index < metrics.length - 1 ? 'mb-3' : ''}
            >
              <Flex
                justifyContent="between"
                alignItems="center"
                className="mb-1"
              >
                <Flex alignItems="center">
                  <FontAwesomeIcon
                    icon={metric.icon as any}
                    className={`text-${metric.variant} me-2`}
                  />
                  <small className="text-700">{metric.label}</small>
                </Flex>
                <small className="fw-bold">{metric.value}</small>
              </Flex>
              <ProgressBar
                variant={metric.variant}
                now={metric.percentage}
                style={{ height: '6px' }}
              />
            </div>
          ))}
        </Card.Body>
      </Card>

      <Card className="mb-3">
        <Card.Header className="bg-body-tertiary">
          <h5 className="mb-0">
            <FontAwesomeIcon icon="chart-pie" className="me-2" />
            {t('adminUserProfile.metrics.systemUsageTitle')}
          </h5>
        </Card.Header>
        <Card.Body>
          {systemUsage.map((item, index) => (
            <div
              key={index}
              className={index < systemUsage.length - 1 ? 'mb-3' : ''}
            >
              <Flex justifyContent="between" className="mb-1">
                <small className="text-700">{item.feature}</small>
                <small className="fw-bold">
                  {t('adminUserProfile.metrics.percent', { value: item.usage })}
                </small>
              </Flex>
              <ProgressBar
                variant={item.color}
                now={item.usage}
                style={{ height: '4px' }}
              />
            </div>
          ))}
        </Card.Body>
      </Card>

      <Card>
        <Card.Header className="bg-body-tertiary">
          <h5 className="mb-0">
            <FontAwesomeIcon icon="info-circle" className="me-2" />
            {t('adminUserProfile.metrics.quickStatsTitle')}
          </h5>
        </Card.Header>
        <Card.Body>
          <Row className="g-2">
            <Col xs={6}>
              <div className="text-center p-2 bg-primary bg-opacity-10 rounded">
                <div className="fw-bold text-primary fs-7">
                  {metricsData.quickStats.loginCount}
                </div>
                <small className="text-600">
                  {t('adminUserProfile.metrics.statLogins')}
                </small>
              </div>
            </Col>
            <Col xs={6}>
              <div className="text-center p-2 bg-success bg-opacity-10 rounded">
                <div className="fw-bold text-success fs-7">
                  {t('adminUserProfile.metrics.statOnlineTimeHours', {
                    hours: metricsData.quickStats.onlineTimeHours
                  })}
                </div>
                <small className="text-600">
                  {t('adminUserProfile.metrics.statOnlineTime')}
                </small>
              </div>
            </Col>
            <Col xs={6}>
              <div className="text-center p-2 bg-info bg-opacity-10 rounded">
                <div className="fw-bold text-info fs-7">
                  {metricsData.quickStats.activeTasks}
                </div>
                <small className="text-600">
                  {t('adminUserProfile.metrics.statActiveTasks')}
                </small>
              </div>
            </Col>
            <Col xs={6}>
              <div className="text-center p-2 bg-warning bg-opacity-10 rounded">
                <div className="fw-bold text-warning fs-7">
                  {metricsData.quickStats.overdueTasks}
                </div>
                <small className="text-600">
                  {t('adminUserProfile.metrics.statOverdue')}
                </small>
              </div>
            </Col>
          </Row>
        </Card.Body>
      </Card>
    </>
  );
};

export default AdminUserMetrics;
