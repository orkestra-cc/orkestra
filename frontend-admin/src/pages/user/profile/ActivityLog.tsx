import { Card } from 'react-bootstrap';
import Notification from 'components/notification/Notification';
import classNames from 'classnames';
import Flex from 'components/common/Flex';
import { Link } from 'react-router';
import { useTranslation } from 'react-i18next';
import paths from 'routes/paths';

interface ActivityLogProps {
  activities: any[];
  [key: string]: any;
}

const ActivityLog = ({ activities, ...rest }: ActivityLogProps) => {
  const { t } = useTranslation();
  return (
    <Card {...rest}>
      <Card.Header className="bg-body-tertiary">
        <Flex justifyContent="between">
          <h5 className="mb-1 mb-md-0">
            {t('userProfileScaffold.activityLog.title')}
          </h5>
          <Link to={paths.activityLog} className="font-sans-serif">
            {t('userProfileScaffold.activityLog.allLogs')}
          </Link>
        </Flex>
      </Card.Header>
      <Card.Body className="p-0">
        {activities.map((activity: any, index: number) => (
          <Notification
            {...activity}
            key={activity.id}
            className={classNames(
              'border-x-0 border-bottom-0 border-300',
              index + 1 === activities.length ? 'rounded-top-0' : 'rounded-0'
            )}
          />
        ))}
      </Card.Body>
    </Card>
  );
};

export default ActivityLog;
