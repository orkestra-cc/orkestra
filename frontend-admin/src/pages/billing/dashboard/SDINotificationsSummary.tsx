import { Card, Spinner, ListGroup } from 'react-bootstrap';
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome';
import {
  faCheck,
  faTimes,
  faInfoCircle,
  faExclamationTriangle,
  faClock,
} from '@fortawesome/free-solid-svg-icons';
import FalconCardHeader from 'components/common/FalconCardHeader';
import { Link } from 'react-router';
import { useGetNotificationSummaryQuery } from 'store/api/billingApi';
import { NOTIFICATION_TYPE_LABELS } from 'types/billing';
import type { NotificationType } from 'types/billing';
import { lastYearRange } from './dateRanges';

const getNotificationIcon = (type: NotificationType) => {
  switch (type) {
    case 'RC':
      return { icon: faCheck, color: 'text-success' };
    case 'NS':
      return { icon: faTimes, color: 'text-danger' };
    case 'MC':
      return { icon: faInfoCircle, color: 'text-info' };
    case 'NE':
      return { icon: faExclamationTriangle, color: 'text-warning' };
    case 'DT':
      return { icon: faClock, color: 'text-secondary' };
    case 'AT':
      return { icon: faCheck, color: 'text-primary' };
    default:
      return { icon: faInfoCircle, color: 'text-body-tertiary' };
  }
};

const SDINotificationsSummary = () => {
  const { data: summary, isLoading, error } = useGetNotificationSummaryQuery(lastYearRange());

  if (isLoading) {
    return (
      <Card className="h-100">
        <FalconCardHeader title="Notifiche SDI" titleTag="h6" light />
        <Card.Body className="d-flex align-items-center justify-content-center" style={{ minHeight: 200 }}>
          <Spinner animation="border" size="sm" />
        </Card.Body>
      </Card>
    );
  }

  if (error || !summary) {
    return (
      <Card className="h-100">
        <FalconCardHeader title="Notifiche SDI" titleTag="h6" light />
        <Card.Body className="d-flex align-items-center justify-content-center text-warning" style={{ minHeight: 200 }}>
          <div className="text-center">
            <FontAwesomeIcon icon={faExclamationTriangle} className="fs-3 mb-2 d-block" />
            <span className="fs-10">Impossibile caricare</span>
          </div>
        </Card.Body>
      </Card>
    );
  }

  // Convert summary object to array format for display
  const notificationTypes: { type: NotificationType; count: number }[] = (
    [
      { type: 'RC' as NotificationType, count: summary.RC || 0 },
      { type: 'NS' as NotificationType, count: summary.NS || 0 },
      { type: 'MC' as NotificationType, count: summary.MC || 0 },
      { type: 'NE' as NotificationType, count: summary.NE || 0 },
      { type: 'DT' as NotificationType, count: summary.DT || 0 },
      { type: 'AT' as NotificationType, count: summary.AT || 0 },
    ] as { type: NotificationType; count: number }[]
  ).filter(item => item.count > 0);

  const totalUnprocessed = summary.unprocessed || 0;

  return (
    <Card className="h-100">
      <FalconCardHeader
        title="Notifiche SDI"
        titleTag="h6"
        light
        endEl={
          totalUnprocessed > 0 && (
            <span className="badge bg-warning rounded-pill fs-10">
              {totalUnprocessed} da gestire
            </span>
          )
        }
      />
      <Card.Body className="px-0 py-0">
        {notificationTypes.length === 0 ? (
          <div className="text-center py-4 text-body-tertiary">
            <FontAwesomeIcon icon={faCheck} className="fs-3 mb-2 d-block text-success" />
            <span className="fs-10">Nessuna notifica recente</span>
          </div>
        ) : (
          <ListGroup variant="flush">
            {notificationTypes.map(({ type, count }) => {
              const { icon, color } = getNotificationIcon(type);
              return (
                <ListGroup.Item
                  key={type}
                  className="d-flex justify-content-between align-items-center px-x1 py-2"
                >
                  <div className="d-flex align-items-center">
                    <FontAwesomeIcon icon={icon} className={`${color} me-2`} fixedWidth />
                    <span className="fs-10">{NOTIFICATION_TYPE_LABELS[type]}</span>
                  </div>
                  <span className="badge bg-body-secondary text-body rounded-pill">
                    {count}
                  </span>
                </ListGroup.Item>
              );
            })}
          </ListGroup>
        )}
      </Card.Body>
      <Card.Footer className="bg-body-tertiary py-2">
        <Link to="/billing/notifications" className="fw-semibold fs-10">
          Vedi tutte le notifiche
          <FontAwesomeIcon icon="angle-right" className="ms-1" transform="down-1" />
        </Link>
      </Card.Footer>
    </Card>
  );
};

export default SDINotificationsSummary;
