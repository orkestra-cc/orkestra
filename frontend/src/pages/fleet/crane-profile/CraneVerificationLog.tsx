
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome';
import { Card, Badge, ListGroup, Button } from 'react-bootstrap';
import { FaCheckCircle, FaExclamationTriangle, FaClipboardCheck, FaClock, FaCalendarAlt, FaFileAlt } from 'react-icons/fa';

interface CraneVerificationLogProps {
  craneId: string;
  className?: string;
}

// Mock data for verification activities (in real app, this would come from API)
const mockVerificationData = [
  {
    id: '1',
    type: 'verifica_periodica',
    action: 'Annual Periodic Verification Completed',
    timestamp: '2024-01-15T10:30:00',
    description: 'Complete verification according to INAIL regulations. Positive result.',
    status: 'completed',
    operator: 'Eng. Mario Rossi',
    icon: <FaClipboardCheck />
  },
  {
    id: '2',
    type: 'verifica_trimestrale',
    action: 'Quarterly Cable Verification',
    timestamp: '2024-01-10T14:20:00',
    description: 'Cable and chain integrity check. No anomalies detected.',
    status: 'completed',
    operator: 'Tech. Luigi Bianchi',
    icon: <FaCheckCircle />
  },
  {
    id: '3',
    type: 'ispezione',
    action: 'Extraordinary Inspection',
    timestamp: '2023-12-20T09:15:00',
    description: 'Post-maintenance inspection of hydraulic system.',
    status: 'completed',
    operator: 'Eng. Giuseppe Verdi',
    icon: <FaFileAlt />
  },
  {
    id: '4',
    type: 'programmata',
    action: 'Scheduled Verification',
    timestamp: '2024-03-15T00:00:00',
    description: 'Next scheduled quarterly verification',
    status: 'scheduled',
    icon: <FaCalendarAlt />
  },
  {
    id: '5',
    type: 'scadenza',
    action: 'Expiry Warning',
    timestamp: '2023-11-30T11:00:00',
    description: 'Verification expiring within 30 days',
    status: 'warning',
    icon: <FaExclamationTriangle />
  }
];

const CraneVerificationLog: React.FC<CraneVerificationLogProps> = ({ className = '' }) => {
  const formatDate = (timestamp: string) => {
    const date = new Date(timestamp);
    const now = new Date();
    const diffInHours = Math.floor((now.getTime() - date.getTime()) / (1000 * 60 * 60));

    // Future date
    if (diffInHours < 0) {
      const futureDiff = Math.abs(diffInHours);
      if (futureDiff < 24) {
        return `in ${futureDiff} hours`;
      } else if (futureDiff < 720) {
        return `in ${Math.floor(futureDiff / 24)} days`;
      } else {
        return date.toLocaleDateString('en-GB', {
          day: 'numeric',
          month: 'short',
          year: 'numeric'
        });
      }
    }

    // Past date
    if (diffInHours < 24) {
      return `${diffInHours} hours ago`;
    } else if (diffInHours < 48) {
      return '1 day ago';
    } else if (diffInHours < 720) {
      return `${Math.floor(diffInHours / 24)} days ago`;
    } else {
      return date.toLocaleDateString('en-GB', {
        day: 'numeric',
        month: 'short',
        year: 'numeric'
      });
    }
  };

  const getStatusBadge = (type: string) => {
    const badges: Record<string, { bg: string; text: string }> = {
      verifica_periodica: { bg: 'success', text: 'Annual Verification' },
      verifica_trimestrale: { bg: 'primary', text: 'Quarterly Verification' },
      ispezione: { bg: 'info', text: 'Inspection' },
      programmata: { bg: 'warning', text: 'Scheduled' },
      scadenza: { bg: 'danger', text: 'Expiring' }
    };

    const badge = badges[type] || { bg: 'secondary', text: 'Other' };
    return <Badge bg={badge.bg}>{badge.text}</Badge>;
  };

  const getStatusColor = (status: string) => {
    const colors: Record<string, string> = {
      completed: 'success',
      scheduled: 'warning',
      warning: 'danger',
      info: 'info'
    };
    return colors[status] || 'secondary';
  };

  return (
    <Card className={className}>
      <Card.Header className="bg-body-tertiary d-flex justify-content-between align-items-center">
        <h5 className="mb-0">
          <FaClock className="me-2" />
          Verification History
        </h5>
        <Button variant="falcon-primary" size="sm">
          <FontAwesomeIcon icon="plus" className="me-1" />
          New Verification
        </Button>
      </Card.Header>
      <Card.Body className="p-0">
        <ListGroup variant="flush">
          {mockVerificationData.map((activity, index) => (
            <ListGroup.Item key={activity.id} className="px-3 py-3">
              <div className="d-flex">
                <div className="me-3">
                  <div
                    className={`icon-circle bg-${getStatusColor(activity.status)} text-white d-flex align-items-center justify-content-center`}
                    style={{ width: '32px', height: '32px', borderRadius: '50%', fontSize: '14px' }}
                  >
                    {activity.icon}
                  </div>
                  {index < mockVerificationData.length - 1 && (
                    <div
                      className="bg-300 mx-auto"
                      style={{ width: '2px', height: '40px', marginTop: '8px' }}
                    />
                  )}
                </div>
                <div className="flex-1">
                  <div className="d-flex justify-content-between align-items-start mb-1">
                    <div>
                      <h6 className="mb-0 text-900">{activity.action}</h6>
                      <p className="fs-10 text-600 mb-1">
                        {activity.description}
                      </p>
                      {activity.operator && (
                        <small className="text-muted">
                          <FontAwesomeIcon icon="user" className="me-1" />
                          {activity.operator}
                        </small>
                      )}
                    </div>
                    <div className="text-end">
                      {getStatusBadge(activity.type)}
                      <div className="fs-11 text-muted mt-1">
                        {formatDate(activity.timestamp)}
                      </div>
                    </div>
                  </div>
                </div>
              </div>
            </ListGroup.Item>
          ))}
        </ListGroup>
      </Card.Body>
      <Card.Footer className="bg-body-tertiary p-3">
        <Button variant="link" size="sm" className="p-0">
          View all verifications
          <FontAwesomeIcon icon="angle-right" className="ms-1" />
        </Button>
      </Card.Footer>
    </Card>
  );
};

export default CraneVerificationLog;