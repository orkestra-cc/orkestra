import { ReactNode } from 'react';
import classNames from 'classnames';
import { Link } from 'react-router';
import createMarkup from 'helpers/createMarkup';
import Avatar from 'components/common/Avatar';

type AvatarSize = 'xs' | 's' | 'm' | 'l' | 'xl' | '2xl' | '3xl' | '4xl' | '5xl';
type AvatarRounded = 'circle' | '0' | '1' | '2' | '3' | 'pill' | 'top' | 'end' | 'bottom' | 'start';

interface NotificationAvatarProps {
  size?: AvatarSize;
  rounded?: AvatarRounded;
  src?: string | string[];
  name?: string;
  emoji?: string;
  className?: string;
  mediaClass?: string;
  isExact?: boolean;
}

interface NotificationProps {
  avatar?: NotificationAvatarProps;
  time?: string;
  className?: string;
  unread?: boolean;
  flush?: boolean;
  emoji?: string;
  children?: ReactNode;
}

const Notification = ({
  avatar,
  time,
  className,
  unread = false,
  flush = false,
  emoji,
  children
}: NotificationProps) => (
  <Link
    className={classNames(
      'notification',
      { 'notification-unread': unread, 'notification-flush': flush },
      className
    )}
    to="#!"
  >
    {avatar && (
      <div className="notification-avatar">
        <Avatar {...avatar} className="me-3" />
      </div>
    )}
    <div className="notification-body">
      <p className="mb-1" dangerouslySetInnerHTML={createMarkup(String(children || ''))} />
      <span className="notification-time">
        {emoji && (
          <span className="me-2" role="img" aria-label="Emoji">
            {emoji}
          </span>
        )}
        {time}
      </span>
    </div>
  </Link>
);

export default Notification;
