import { Card, ListGroup, Spinner, Badge } from 'react-bootstrap';
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome';
import {
  faFileInvoice,
  faBell,
  faExclamationCircle,
  faCheckCircle,
  faArrowRight
} from '@fortawesome/free-solid-svg-icons';
import { useTranslation } from 'react-i18next';
import OrkestraCardHeader from 'components/common/OrkestraCardHeader';
import { Link } from 'react-router';
import {
  useGetBillingStatsQuery,
  useGetNotificationsQuery
} from 'store/api/billingApi';
import { lastYearRange } from './dateRanges';

interface PendingAction {
  id: string;
  title: string;
  description: string;
  icon: any;
  iconColor: string;
  link: string;
  priority: 'high' | 'medium' | 'low';
}

const PendingActions = () => {
  const { t } = useTranslation();
  const lastYear = lastYearRange();
  const { data: stats, isLoading: statsLoading } =
    useGetBillingStatsQuery(lastYear);
  const { isLoading: notificationsLoading } = useGetNotificationsQuery({
    processed: false,
    pageSize: 5,
    fromDate: lastYear.fromDate,
    toDate: lastYear.toDate
  });

  const isLoading = statsLoading || notificationsLoading;

  if (isLoading) {
    return (
      <Card className="h-100">
        <OrkestraCardHeader
          title={t('billing.dashboard.pendingActions.title')}
          titleTag="h6"
          light
        />
        <Card.Body
          className="d-flex align-items-center justify-content-center"
          style={{ minHeight: 200 }}
        >
          <Spinner animation="border" size="sm" />
        </Card.Body>
      </Card>
    );
  }

  const actions: PendingAction[] = [];

  // Add draft invoices action
  if (stats && stats.issuedDraft > 0) {
    actions.push({
      id: 'drafts',
      title: t('billing.dashboard.pendingActions.drafts.title'),
      description: t('billing.dashboard.pendingActions.drafts.description', {
        count: stats.issuedDraft
      }),
      icon: faFileInvoice,
      iconColor: 'text-warning',
      link: '/billing/invoices/issued?status=draft',
      priority: 'medium'
    });
  }

  // Add rejected invoices action
  if (stats && stats.issuedRejected > 0) {
    actions.push({
      id: 'rejected',
      title: t('billing.dashboard.pendingActions.rejected.title'),
      description: t('billing.dashboard.pendingActions.rejected.description', {
        count: stats.issuedRejected
      }),
      icon: faExclamationCircle,
      iconColor: 'text-danger',
      link: '/billing/invoices/issued?status=rejected',
      priority: 'high'
    });
  }

  // Add pending received invoices
  if (stats && stats.receivedPending > 0) {
    actions.push({
      id: 'receivedPending',
      title: t('billing.dashboard.pendingActions.receivedPending.title'),
      description: t(
        'billing.dashboard.pendingActions.receivedPending.description',
        { count: stats.receivedPending }
      ),
      icon: faFileInvoice,
      iconColor: 'text-info',
      link: '/billing/invoices/received?status=pending',
      priority: 'medium'
    });
  }

  // Add unprocessed notifications
  if (stats && stats.unprocessedNotifications > 0) {
    actions.push({
      id: 'notifications',
      title: t('billing.dashboard.pendingActions.notifications.title'),
      description: t(
        'billing.dashboard.pendingActions.notifications.description',
        { count: stats.unprocessedNotifications }
      ),
      icon: faBell,
      iconColor: 'text-warning',
      link: '/billing/notifications?processed=false',
      priority: 'medium'
    });
  }

  // Sort by priority
  const priorityOrder = { high: 0, medium: 1, low: 2 };
  actions.sort((a, b) => priorityOrder[a.priority] - priorityOrder[b.priority]);

  const getPriorityBadge = (priority: PendingAction['priority']) => {
    switch (priority) {
      case 'high':
        return (
          <Badge bg="danger" className="fs-11">
            {t('billing.dashboard.pendingActions.priority.high')}
          </Badge>
        );
      case 'medium':
        return (
          <Badge bg="warning" className="fs-11">
            {t('billing.dashboard.pendingActions.priority.medium')}
          </Badge>
        );
      case 'low':
        return (
          <Badge bg="secondary" className="fs-11">
            {t('billing.dashboard.pendingActions.priority.low')}
          </Badge>
        );
    }
  };

  return (
    <Card className="h-100">
      <OrkestraCardHeader
        title={t('billing.dashboard.pendingActions.title')}
        titleTag="h6"
        light
        endEl={
          actions.length > 0 && (
            <span className="badge bg-danger rounded-pill fs-10">
              {actions.length}
            </span>
          )
        }
      />
      <Card.Body className="px-0 py-0">
        {actions.length === 0 ? (
          <div className="text-center py-4">
            <FontAwesomeIcon
              icon={faCheckCircle}
              className="fs-3 mb-2 d-block text-success"
            />
            <p className="text-success fs-10 mb-0 fw-medium">
              {t('billing.dashboard.pendingActions.empty')}
            </p>
            <p className="text-body-tertiary fs-11 mb-0">
              {t('billing.dashboard.pendingActions.emptySubtitle')}
            </p>
          </div>
        ) : (
          <ListGroup variant="flush">
            {actions.map(action => (
              <ListGroup.Item
                key={action.id}
                as={Link}
                to={action.link}
                className="d-flex align-items-center px-x1 py-2 text-decoration-none"
                action
              >
                <div
                  className={`d-flex align-items-center justify-content-center rounded-circle bg-body-secondary me-3`}
                  style={{ width: 36, height: 36 }}
                >
                  <FontAwesomeIcon
                    icon={action.icon}
                    className={action.iconColor}
                  />
                </div>
                <div className="flex-grow-1">
                  <div className="d-flex justify-content-between align-items-start">
                    <h6 className="mb-0 fs-10">{action.title}</h6>
                    {getPriorityBadge(action.priority)}
                  </div>
                  <p className="mb-0 text-body-tertiary fs-11">
                    {action.description}
                  </p>
                </div>
                <FontAwesomeIcon
                  icon={faArrowRight}
                  className="text-body-tertiary ms-2"
                />
              </ListGroup.Item>
            ))}
          </ListGroup>
        )}
      </Card.Body>
    </Card>
  );
};

export default PendingActions;
