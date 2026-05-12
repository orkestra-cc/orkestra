import OrkestraCardHeader from 'components/common/OrkestraCardHeader';
import { Card } from 'react-bootstrap';
import OrkestraCardFooterLink from 'components/common/OrkestraCardFooterLink';
import Event from 'reference/app-examples/events/event-list/Event';
import paths from 'routes/paths';

interface EventsProps {
  cardTitle: string;
  events: any[];
  [key: string]: any;
}

const Events = ({ cardTitle, events, ...rest }: EventsProps) => {
  return (
    <Card {...rest}>
      <OrkestraCardHeader title={cardTitle} light />
      <Card.Body className="fs-10 border-bottom">
        {events.map((event: any, index: number) => (
          <Event
            key={event.id}
            details={event}
            isLast={index === events.length - 1}
          />
        ))}
      </Card.Body>
      <OrkestraCardFooterLink title="All Events" to={paths.eventList} size="sm" />
    </Card>
  );
};

export default Events;
