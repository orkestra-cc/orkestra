import Calendar from 'components/common/Calendar';
import Flex from 'components/common/Flex';
import { Link } from 'react-router';
import paths from 'routes/paths';

interface BadgeType {
  title?: string;
  type?: string;
}

interface CalendarType {
  month: string;
  day: string;
}

interface EventDetailsType {
  calendar: CalendarType;
  title: string;
  organizer: string;
  badge?: BadgeType;
  time: string;
  place: string;
  location?: string;
  duration?: string;
  interested?: string;
}

interface EventProps {
  details: EventDetailsType;
  isLast: boolean;
}

const Event = ({ details, isLast }: EventProps) => {
  const {
    calendar,
    title,
    organizer,
    badge = {},
    time,
    place,
    location,
    duration,
    interested
  } = details;
  return (
    <Flex>
      <Calendar {...calendar} />
      <div className="flex-1 position-relative ps-3">
        <h6 className="fs-9 mb-0">
          <Link to={paths.eventDetail}>
            <span className="me-1">{title}</span>
            {badge.title && (
              <span className={`badge badge-${badge.type} rounded-pill `}>
                {badge.title}
              </span>
            )}
          </Link>
        </h6>
        <p className="mb-1">
          Organized by
          <Link to="#!" className="text-700 ps-1">
            {organizer}
          </Link>
        </p>
        <p className="text-1000 mb-0">Time: {time}</p>

        {duration && <p className="text-1000 mb-0">Duration: {duration}</p>}
        {interested && <p className="text-1000 mb-0">{interested}</p>}
        {location && <p className="text-1000 mb-0">Location: {location}</p>}

        <p className="mb-0">Place: {place}</p>
        {!isLast && <div className="border-dashed border-bottom my-3"></div>}
      </div>
    </Flex>
  );
};

export default Event;
