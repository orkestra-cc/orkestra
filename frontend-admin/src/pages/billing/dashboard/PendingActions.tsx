import { Card, ListGroup, Spinner, Badge } from 'react-bootstrap';
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome';
import {
  faFileInvoice,
  faBell,
  faExclamationCircle,
  faCheckCircle,
  faArrowRight,
} from '@fortawesome/free-solid-svg-icons';
import FalconCardHeader from 'components/common/FalconCardHeader';
import { Link } from 'react-router';
import { useGetBillingStatsQuery, useGetNotificationsQuery } from 'store/api/billingApi';

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
  const { data: stats, isLoading: statsLoading } = useGetBillingStatsQuery({});
  const { isLoading: notificationsLoading } = useGetNotificationsQuery({
    processed: false,
    pageSize: 5,
  });

  const isLoading = statsLoading || notificationsLoading;

  if (isLoading) {
    return (
      <Card className="h-100">
        <FalconCardHeader title="Azioni Richieste" titleTag="h6" light />
        <Card.Body className="d-flex align-items-center justify-content-center" style={{ minHeight: 200 }}>
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
      title: 'Fatture in Bozza',
      description: `${stats.issuedDraft} fatture da completare`,
      icon: faFileInvoice,
      iconColor: 'text-warning',
      link: '/billing/invoices/issued?status=draft',
      priority: 'medium',
    });
  }

  // Add rejected invoices action
  if (stats && stats.issuedRejected > 0) {
    actions.push({
      id: 'rejected',
      title: 'Fatture Rifiutate',
      description: `${stats.issuedRejected} fatture da correggere`,
      icon: faExclamationCircle,
      iconColor: 'text-danger',
      link: '/billing/invoices/issued?status=rejected',
      priority: 'high',
    });
  }

  // Add pending received invoices
  if (stats && stats.receivedPending > 0) {
    actions.push({
      id: 'receivedPending',
      title: 'Fatture da Registrare',
      description: `${stats.receivedPending} fatture ricevute da gestire`,
      icon: faFileInvoice,
      iconColor: 'text-info',
      link: '/billing/invoices/received?status=pending',
      priority: 'medium',
    });
  }

  // Add unprocessed notifications
  if (stats && stats.unprocessedNotifications > 0) {
    actions.push({
      id: 'notifications',
      title: 'Notifiche SDI',
      description: `${stats.unprocessedNotifications} notifiche da processare`,
      icon: faBell,
      iconColor: 'text-warning',
      link: '/billing/notifications?processed=false',
      priority: 'medium',
    });
  }

  // Sort by priority
  const priorityOrder = { high: 0, medium: 1, low: 2 };
  actions.sort((a, b) => priorityOrder[a.priority] - priorityOrder[b.priority]);

  const getPriorityBadge = (priority: PendingAction['priority']) => {
    switch (priority) {
      case 'high':
        return <Badge bg="danger" className="fs-11">Urgente</Badge>;
      case 'medium':
        return <Badge bg="warning" className="fs-11">Medio</Badge>;
      case 'low':
        return <Badge bg="secondary" className="fs-11">Basso</Badge>;
    }
  };

  return (
    <Card className="h-100">
      <FalconCardHeader
        title="Azioni Richieste"
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
              Nessuna azione pendente
            </p>
            <p className="text-body-tertiary fs-11 mb-0">
              Tutto in ordine!
            </p>
          </div>
        ) : (
          <ListGroup variant="flush">
            {actions.map((action) => (
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
