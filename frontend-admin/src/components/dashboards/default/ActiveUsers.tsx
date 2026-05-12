import OrkestraCardFooterLink from 'components/common/OrkestraCardFooterLink';
import OrkestraCardHeader from 'components/common/OrkestraCardHeader';
import CardDropdown from 'components/common/CardDropdown';
import { Card, CardProps } from 'react-bootstrap';
import Flex from 'components/common/Flex';
import { Link } from 'react-router';
import Avatar from 'components/common/Avatar';
import classNames from 'classnames';
import paths from 'routes/paths';

interface UserAvatar {
  src?: string;
  name?: string;
  status: string;
}

interface User {
  id: string | number;
  name: string;
  avatar: UserAvatar;
  role: string;
}

interface ActiveUsersProps extends CardProps {
  users: User[];
  end?: number;
}

const ActiveUsers = ({ users, end = 5, ...rest }: ActiveUsersProps) => {
  return (
    <Card {...rest}>
      <OrkestraCardHeader
        light
        title="Active Users"
        titleTag="h6"
        className="py-2"
        endEl={<CardDropdown />}
      />
      <Card.Body className="py-2">
        {users.slice(0, end).map((user: User, index: number) => (
          <ActiveUser
            key={user.id}
            name={user.name}
            avatar={user.avatar}
            role={user.role}
            isLast={index === users.length - 1}
          />
        ))}
      </Card.Body>
      <OrkestraCardFooterLink
        title="All active users"
        to={paths.followers}
        size="sm"
      />
    </Card>
  );
};

interface ActiveUserProps {
  name: string;
  avatar: UserAvatar;
  role: string;
  isLast: boolean;
}

const ActiveUser = ({ name, avatar, role, isLast }: ActiveUserProps) => (
  <Flex
    className={classNames('align-items-center position-relative', {
      'mb-3': !isLast
    })}
  >
    <Avatar {...avatar} className={`status-${avatar.status}`} />
    <div className="ms-3">
      <h6 className="mb-0 fw-semibold">
        <Link className="text-900 stretched-link" to={paths.userProfile}>
          {name}
        </Link>
      </h6>
      <p className="text-500 fs-11 mb-0">{role}</p>
    </div>
  </Flex>
);

export default ActiveUsers;
