import React, { useState } from 'react';
import {
  Card,
  Badge,
  Button,
  ButtonGroup,
  Alert,
  Spinner
} from 'react-bootstrap';
import classNames from 'classnames';
import Flex from 'components/common/Flex';
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome';
import { useGetUserActivitiesQuery } from 'store/api/userApi';

interface AdminActivityLogProps {
  userId: string;
  className?: string;
}

const AdminActivityLog: React.FC<AdminActivityLogProps> = ({
  userId,
  ...rest
}) => {
  const [filter, setFilter] = useState('all');

  const {
    data: activitiesResponse,
    isLoading,
    error
  } = useGetUserActivitiesQuery({
    userId,
    page: 1,
    pageSize: 10,
    type: filter === 'all' ? undefined : filter
  });

  // Helper function to format timestamp
  const formatTimestamp = (timestamp: string) => {
    const date = new Date(timestamp);
    const now = new Date();
    const diffInHours = Math.floor(
      (now.getTime() - date.getTime()) / (1000 * 60 * 60)
    );

    if (diffInHours < 1) return "Less than an hour ago";
    if (diffInHours < 24) return `${diffInHours} hours ago`;
    if (diffInHours < 48) return '1 day ago';
    return `${Math.floor(diffInHours / 24)} days ago`;
  };

  const activities = activitiesResponse?.activities || [];

  const getActivityIcon = (type: string) => {
    switch (type) {
      case 'login':
        return 'sign-in-alt';
      case 'profile':
        return 'user-edit';
      case 'security':
        return 'shield-alt';
      case 'task':
        return 'tasks';
      case 'permission':
        return 'user-cog';
      default:
        return 'circle';
    }
  };

  const getStatusVariant = (status: string) => {
    switch (status) {
      case 'success':
        return 'success';
      case 'warning':
        return 'warning';
      case 'info':
        return 'info';
      case 'danger':
        return 'danger';
      default:
        return 'secondary';
    }
  };

  return (
    <Card {...rest}>
      <Card.Header className="bg-body-tertiary">
        <Flex justifyContent="between" alignItems="center">
          <h5 className="mb-0">Activity Log</h5>
          <ButtonGroup size="sm">
            <Button
              variant={filter === 'all' ? 'primary' : 'outline-primary'}
              onClick={() => setFilter('all')}
            >
              All
            </Button>
            <Button
              variant={filter === 'login' ? 'primary' : 'outline-primary'}
              onClick={() => setFilter('login')}
            >
              Login
            </Button>
            <Button
              variant={filter === 'security' ? 'primary' : 'outline-primary'}
              onClick={() => setFilter('security')}
            >
              Security
            </Button>
            <Button
              variant={filter === 'task' ? 'primary' : 'outline-primary'}
              onClick={() => setFilter('task')}
            >
              Tasks
            </Button>
          </ButtonGroup>
        </Flex>
      </Card.Header>
      <Card.Body className="p-0">
        {isLoading ? (
          <div className="p-3 text-center">
            <Spinner animation="border" size="sm" />
            <span className="ms-2">Loading activities...</span>
          </div>
        ) : error ? (
          <div className="p-3">
            <Alert variant="danger" className="mb-0">
              No activities
            </Alert>
          </div>
        ) : activities.length === 0 ? (
          <div className="p-3 text-center text-muted">
            No activities found
          </div>
        ) : (
          activities.map((activity, index) => (
            <div
              key={activity.id}
              className={classNames(
                'p-3 border-bottom border-300',
                index === activities.length - 1 ? 'border-bottom-0' : ''
              )}
            >
              <Flex>
                <div className="me-3">
                  <div
                    className={`bg-${getStatusVariant(activity.status)} rounded-circle p-2 d-flex align-items-center justify-content-center`}
                    style={{ width: '32px', height: '32px' }}
                  >
                    <FontAwesomeIcon
                      icon={getActivityIcon(activity.type)}
                      className="text-white"
                      size="sm"
                    />
                  </div>
                </div>
                <div className="flex-1">
                  <Flex
                    justifyContent="between"
                    alignItems="start"
                    className="mb-1"
                  >
                    <h6 className="mb-0">{activity.action}</h6>
                    <Badge bg={getStatusVariant(activity.status)}>
                      {activity.type}
                    </Badge>
                  </Flex>
                  <p className="text-600 mb-1 fs-10">
                    <FontAwesomeIcon icon="clock" className="me-1" />
                    {formatTimestamp(activity.timestamp)}
                  </p>
                  <div className="text-500 fs-11">
                    <span className="me-3">
                      <FontAwesomeIcon icon="globe" className="me-1" />
                      IP: {activity.ipAddress}
                    </span>
                    <span>
                      <FontAwesomeIcon icon="desktop" className="me-1" />
                      {activity.device}
                    </span>
                  </div>
                </div>
              </Flex>
            </div>
          ))
        )}
      </Card.Body>
      <Card.Footer className="bg-body-tertiary text-center">
        <Button variant="link" size="sm">
          View Complete History
          <FontAwesomeIcon icon="external-link-alt" className="ms-1" />
        </Button>
      </Card.Footer>
    </Card>
  );
};

export default AdminActivityLog;
