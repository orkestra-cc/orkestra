
import { useAppContext } from 'providers/AppProvider';
import { Modal } from 'react-bootstrap';
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome';
import { Link } from 'react-router';
import Flex from 'components/common/Flex';
import dayjs from 'dayjs';
import paths from 'routes/paths';
import { IconProp } from '@fortawesome/fontawesome-svg-core';
import { ReactNode } from 'react';

interface Schedule {
  title: string;
}

interface CalendarEvent {
  title: string;
  start: Date | string;
  end?: Date | string;
  extendedProps: {
    description?: string;
    location?: string;
    organizer?: string;
    schedules?: Schedule[];
  };
}

interface ModalEventContent {
  event: CalendarEvent | null;
}

interface EventModalMediaContentProps {
  icon: IconProp;
  heading: string;
  content?: string;
  children?: ReactNode;
}

interface CalendarEventModalProps {
  setIsOpenEventModal: (isOpen: boolean) => void;
  isOpenEventModal: boolean;
  modalEventContent: ModalEventContent;
}

const getCircleStackIcon = (icon: IconProp, transform?: string) => (
  <span className="fa-stack ms-n1 me-3">
    <FontAwesomeIcon icon="circle" className="text-200 fa-stack-2x" />
    <FontAwesomeIcon
      icon={icon}
      transform={transform ?? ''}
      className="text-primary fa-stack-1x"
      inverse
    />
  </span>
);

const EventModalMediaContent = ({ icon, heading, content, children }: EventModalMediaContentProps) => (
  <Flex className="mt-3">
    {getCircleStackIcon(icon)}
    <div className="flex-1">
      <h6>{heading}</h6>
      {children || <p className="mb-0 text-justify">{content}</p>}
    </div>
  </Flex>
);

const CalendarEventModal = ({
  setIsOpenEventModal,
  isOpenEventModal,
  modalEventContent
}: CalendarEventModalProps) => {
  const {
    config: { isDark }
  } = useAppContext();

  const handleClose = () => {
    setIsOpenEventModal(!isOpenEventModal);
  };

  const event = isOpenEventModal ? modalEventContent.event : null;
  const title = event?.title || '';
  const start = event?.start;
  const end = event?.end;
  const description = event?.extendedProps?.description;
  const location = event?.extendedProps?.location;
  const organizer = event?.extendedProps?.organizer;
  const schedules = event?.extendedProps?.schedules;

  return (
    <Modal
      show={isOpenEventModal}
      onHide={handleClose}
      contentClassName="border"
      centered
    >
      <Modal.Header
        closeButton
        closeVariant={isDark ? 'white' : undefined}
        className="bg-body-tertiary px-x1 border-bottom-0"
      >
        <Modal.Title>
          <h5 className="mb-0">{title}</h5>
          {organizer && (
            <p className="mb-0 fs-10 mt-1 fw-normal">
              by <a href="#!">{organizer}</a>
            </p>
          )}
        </Modal.Title>
      </Modal.Header>
      <Modal.Body className="px-x1 pb-x1 pt-1 fs-10">
        {description && (
          <EventModalMediaContent
            icon="align-left"
            heading="Description"
            content={description}
          />
        )}
        {(end || start) && (
          <EventModalMediaContent icon="calendar-check" heading="Date and Time">
            <p className="mb-1">
              {dayjs(start).format('dddd, MMMM DD, YYYY, hh:mm A')}
              {end && (
                <>
                  -<br />
                  {dayjs(end).format('dddd, MMMM DD, YYYY, hh:mm A')}
                </>
              )}
            </p>
          </EventModalMediaContent>
        )}
        {location && (
          <EventModalMediaContent icon="map-marker-alt" heading="Location">
            <div
              className="mb-1"
              dangerouslySetInnerHTML={{ __html: location }}
            />
          </EventModalMediaContent>
        )}
        {schedules && (
          <EventModalMediaContent icon="clock" heading="Schedule">
            <ul className="list-unstyled timeline mb-0">
              {schedules.map((schedule, index) => (
                <li key={index}>{schedule.title}</li>
              ))}
            </ul>
          </EventModalMediaContent>
        )}
      </Modal.Body>
      <Modal.Footer className="bg-body-tertiary px-x1 border-top-0">
        <Link
          to={paths.createEvent}
          className="btn btn-falcon-default btn-sm"
        >
          <FontAwesomeIcon icon="pencil-alt" className="fs-11 me-2" />
          <span>Edit</span>
        </Link>
        <Link
          to={paths.eventDetail}
          className="btn btn-falcon-primary btn-sm"
        >
          <span>See more details</span>
          <FontAwesomeIcon icon="angle-right" className="fs-11 ms-1" />
        </Link>
      </Modal.Footer>
    </Modal>
  );
};

export default CalendarEventModal;
