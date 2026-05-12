import { useEffect, useState } from 'react';
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome';
import classNames from 'classnames';
import { Link } from 'react-router';
import { Card, Dropdown, ListGroup } from 'react-bootstrap';
import {
  rawEarlierNotifications,
  rawNewNotifications
} from 'data/notification/notification';
import { isIterableArray } from 'helpers/utils';
import useFakeFetch from 'hooks/ui/useFakeFetch';
import OrkestraCardHeader from 'components/common/OrkestraCardHeader';
import Notification from 'components/notification/Notification';
import SimpleBar from 'simplebar-react';
import type { AvatarSize } from 'components/common/Avatar';

interface NotificationItem {
  id: string | number;
  avatar?: {
    src?: string;
    size?: AvatarSize;
    name?: string;
    emoji?: string;
  };
  time?: string;
  className?: string;
  unread?: boolean;
  emoji?: string;
  children?: React.ReactNode;
}

const NotificationDropdown = () => {
  // State
  const { data: newNotifications, setData: setNewNotifications } =
    useFakeFetch(rawNewNotifications);
  const { data: earlierNotifications, setData: setEarlierNotifications } =
    useFakeFetch(rawEarlierNotifications);
  const [isOpen, setIsOpen] = useState(false);
  const [isAllRead, setIsAllRead] = useState(false);

  // Handler
  const handleToggle = () => {
    setIsOpen(!isOpen);
  };

  useEffect(() => {
    window.addEventListener(
      'scroll',
      () => {
        window.innerWidth < 1200 && setIsOpen(false);
      },
      { passive: true }
    );
  }, []);

  const markAsRead = (e: React.MouseEvent) => {
    e.preventDefault();

    const updatedNewNotifications = (
      newNotifications as NotificationItem[]
    ).map((notification: NotificationItem) =>
      Object.prototype.hasOwnProperty.call(notification, 'unread')
        ? { ...notification, unread: false }
        : notification
    );
    const updatedEarlierNotifications = (
      earlierNotifications as NotificationItem[]
    ).map((notification: NotificationItem) =>
      Object.prototype.hasOwnProperty.call(notification, 'unread')
        ? { ...notification, unread: false }
        : notification
    );

    setIsAllRead(true);
    setNewNotifications(updatedNewNotifications);
    setEarlierNotifications(updatedEarlierNotifications);
  };

  return (
    <Dropdown navbar={true} as="li" show={isOpen} onToggle={handleToggle}>
      <Dropdown.Toggle
        bsPrefix="toggle"
        as={Link}
        to="#!"
        className={classNames('px-0 nav-link', {
          'notification-indicator notification-indicator-primary': !isAllRead
        })}
      >
        <FontAwesomeIcon icon="bell" transform="shrink-6" className="fs-5" />
      </Dropdown.Toggle>

      <Dropdown.Menu className="dropdown-menu-card dropdown-menu-end dropdown-caret dropdown-caret-bg">
        <Card
          className="dropdown-menu-notification dropdown-menu-end shadow-none"
          style={{ maxWidth: '20rem' }}
        >
          <OrkestraCardHeader
            className="card-header"
            title="Notifications"
            titleTag="h6"
            light={false}
            endEl={
              <Link
                className="card-link fw-normal"
                to="#!"
                onClick={markAsRead}
              >
                Mark all as read
              </Link>
            }
          />
          <SimpleBar style={{ maxHeight: '19rem' }}>
            <ListGroup variant="flush" className="fw-normal fs-10">
              <div className="list-group-title">NEW</div>{' '}
              {isIterableArray(newNotifications) &&
                (newNotifications as NotificationItem[]).map(
                  (notification: NotificationItem) => (
                    <ListGroup.Item
                      key={notification.id}
                      onClick={handleToggle}
                    >
                      <Notification {...notification} flush />
                    </ListGroup.Item>
                  )
                )}
              <div className="list-group-title">EARLIER</div>
              {isIterableArray(earlierNotifications) &&
                (earlierNotifications as NotificationItem[]).map(
                  (notification: NotificationItem) => (
                    <ListGroup.Item
                      key={notification.id}
                      onClick={handleToggle}
                    >
                      <Notification {...notification} flush />
                    </ListGroup.Item>
                  )
                )}
            </ListGroup>
          </SimpleBar>
          <div
            className="card-footer text-center border-top"
            onClick={handleToggle}
          >
            <Link className="card-link d-block" to="#!">
              View all
            </Link>
          </div>
        </Card>
      </Dropdown.Menu>
    </Dropdown>
  );
};

export default NotificationDropdown;
