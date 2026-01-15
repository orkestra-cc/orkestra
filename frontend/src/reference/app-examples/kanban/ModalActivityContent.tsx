
import { activities } from 'data/kanban';
import Flex from 'components/common/Flex';
import { Link } from 'react-router';
import Avatar from 'components/common/Avatar';
import paths from 'routes/paths';
import classNames from 'classnames';

interface Activity {
  id: string;
  user: {
    name: string;
    avatar: string;
  };
  activity: string;
  time: string;
}

const ModalActivityContent = () => {
  return (
    <>
      {(activities as Activity[]).map((activity: Activity, index: number) => (
        <Flex
          key={activity.id}
          className={classNames({
            'mb-3': activities.length - 1 !== index
          })}
        >
          <Link to={paths.userProfile}>
            <Avatar src={activity.user.avatar} size="l" />
          </Link>
          <div className="flex-1 ms-2 fs-10">
            <p className="mb-0">
              <Link to={paths.userProfile} className="fw-semibold">
                {activity.user.name}{' '}
              </Link>
              {activity.activity}
            </p>
            <div className="fs-11">{activity.time}</div>
          </div>
        </Flex>
      ))}
    </>
  );
};

export default ModalActivityContent;
