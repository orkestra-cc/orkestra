
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome';
import { Card, Badge, ListGroup, Button } from 'react-bootstrap';
import { FaTools, FaOilCan, FaTachometerAlt, FaWrench, FaClipboardCheck } from 'react-icons/fa';

interface VehicleMaintenanceLogProps {
  vehicleId: string;
  className?: string;
}

// Mock data for maintenance activities (in real app, this would come from API)
const mockMaintenanceData = [
  {
    id: '1',
    type: 'revisione',
    action: 'Periodic Inspection Completed',
    timestamp: '2024-01-15T10:30:00',
    description: 'Full vehicle inspection at authorized workshop',
    status: 'completed',
    icon: <FaClipboardCheck />
  },
  {
    id: '2',
    type: 'manutenzione',
    action: 'Engine Oil Change',
    timestamp: '2024-01-10T14:20:00',
    description: 'Engine oil and oil filter replacement',
    status: 'completed',
    icon: <FaOilCan />
  },
  {
    id: '3',
    type: 'riparazione',
    action: 'Tire Replacement',
    timestamp: '2023-12-20T09:15:00',
    description: 'Complete set of front and rear tires replaced',
    status: 'completed',
    icon: <FaWrench />
  },
  {
    id: '4',
    type: 'controllo',
    action: 'Mileage Check',
    timestamp: '2023-12-15T16:45:00',
    description: '120,000 km recorded',
    status: 'info',
    icon: <FaTachometerAlt />
  },
  {
    id: '5',
    type: 'manutenzione',
    action: 'Scheduled Service',
    timestamp: '2023-11-30T11:00:00',
    description: 'Full service: oil, filters, fluids',
    status: 'completed',
    icon: <FaTools />
  }
];

const VehicleMaintenanceLog: React.FC<VehicleMaintenanceLogProps> = ({ className = '' }) => {
  const formatDate = (timestamp: string) => {
    const date = new Date(timestamp);
    const now = new Date();
    const diffInHours = Math.floor((now.getTime() - date.getTime()) / (1000 * 60 * 60));

    if (diffInHours < 24) {
      return `${diffInHours} hours ago`;
    } else if (diffInHours < 48) {
      return '1 day ago';
    } else if (diffInHours < 720) { // Less than 30 days
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
      revisione: { bg: 'success', text: 'Inspection' },
      manutenzione: { bg: 'primary', text: 'Maintenance' },
      riparazione: { bg: 'warning', text: 'Repair' },
      controllo: { bg: 'info', text: 'Check' }
    };

    const badge = badges[type] || { bg: 'secondary', text: 'Other' };
    return <Badge bg={badge.bg}>{badge.text}</Badge>;
  };

  const getStatusColor = (status: string) => {
    const colors: Record<string, string> = {
      completed: 'success',
      warning: 'warning',
      info: 'info',
      danger: 'danger'
    };
    return colors[status] || 'secondary';
  };

  return (
    <Card className={className}>
      <Card.Header className="bg-body-tertiary d-flex justify-content-between align-items-center">
        <h5 className="mb-0">
          <FontAwesomeIcon icon="history" className="me-2" />
          Maintenance History
        </h5>
        <Button variant="falcon-default" size="sm">
          <FontAwesomeIcon icon="plus" className="me-1" />
          Add Event
        </Button>
      </Card.Header>
      <Card.Body>
        <div className="mb-3">
          <small className="text-muted">
            Recent maintenance and inspection activities for this vehicle
          </small>
        </div>

        <ListGroup variant="flush">
          {mockMaintenanceData.map((activity) => (
            <ListGroup.Item
              key={activity.id}
              className="px-0 py-3 border-bottom"
            >
              <div className="d-flex align-items-start">
                <div
                  className={`rounded-circle p-2 bg-soft-${getStatusColor(activity.status)} text-${getStatusColor(activity.status)} me-3`}
                  style={{ width: '40px', height: '40px', display: 'flex', alignItems: 'center', justifyContent: 'center' }}
                >
                  {activity.icon}
                </div>
                <div className="flex-1">
                  <div className="d-flex justify-content-between align-items-start mb-1">
                    <div>
                      <h6 className="mb-1">{activity.action}</h6>
                      <p className="mb-1 text-muted small">
                        {activity.description}
                      </p>
                    </div>
                    {getStatusBadge(activity.type)}
                  </div>
                  <div className="text-muted small">
                    <FontAwesomeIcon icon="clock" className="me-1" />
                    {formatDate(activity.timestamp)}
                  </div>
                </div>
              </div>
            </ListGroup.Item>
          ))}
        </ListGroup>

        {mockMaintenanceData.length === 0 && (
          <div className="text-center text-muted py-4">
            <FaTools className="mb-2" size={32} />
            <p>No maintenance records</p>
          </div>
        )}

        {mockMaintenanceData.length > 5 && (
          <div className="text-center mt-3">
            <Button variant="link" size="sm">
              Show full history
              <FontAwesomeIcon icon="arrow-right" className="ms-2" />
            </Button>
          </div>
        )}
      </Card.Body>
    </Card>
  );
};

export default VehicleMaintenanceLog;