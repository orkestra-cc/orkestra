import { useEffect, useRef, useState } from 'react';
import {
  Button,
  Card,
  Col,
  OverlayTrigger,
  Row,
  Tooltip
} from 'react-bootstrap';
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome';
import IconButton from 'components/common/IconButton';
import FullCalendar from '@fullcalendar/react';
import dayGridPlugin from '@fullcalendar/daygrid';
import timeGridPlugin from '@fullcalendar/timegrid';
import interactionPlugin from '@fullcalendar/interaction';
import AddScheduleModal from 'reference/app-examples/calendar/AddScheduleModal';
import CalendarEventModal from 'reference/app-examples/calendar/CalendarEventModal';
import Flex from 'components/common/Flex';
import { useAppContext } from 'providers/AppProvider';

interface CalendarEventData {
  id: string;
  title: string;
  start?: string | Date;
  end?: string | Date;
  startTime?: string;
  endTime?: string;
  color?: string;
  extendedProps?: Record<string, unknown>;
}

interface CalendarManagementProps {
  data: CalendarEventData[];
}

interface ModalEventContent {
  event: {
    title: string;
    start: Date | string;
    end?: Date | string;
    extendedProps: {
      description?: string;
      location?: string;
      organizer?: string;
      schedules?: { title: string }[];
    };
  } | null;
}

const CalendarManagement = ({ data }: CalendarManagementProps) => {
  const {
    config: { isRTL }
  } = useAppContext();
  const calendarRef = useRef<FullCalendar>(null);
  const [title, setTitle] = useState('');
  const [day, setDay] = useState('');
  const [calendarApi, setCalendarApi] = useState<ReturnType<FullCalendar['getApi']> | null>(null);
  const [isOpenScheduleModal, setIsOpenScheduleModal] = useState(false);
  const [isOpenEventModal, setIsOpenEventModal] = useState(false);
  const [modalEventContent, setModalEventContent] = useState<ModalEventContent>({ event: null });
  const [scheduleStartDate, setScheduleStartDate] = useState<Date | null | undefined>();
  const [scheduleEndDate, setScheduleEndDate] = useState<Date | null | undefined>();

  const handleEventClick = (eventsInfo: CalendarEventData) => {
    if (calendarApi) {
      const event = calendarApi.getEventById(String(eventsInfo.id));
      if (event) {
        setModalEventContent({
          event: {
            title: event.title,
            start: event.start || new Date(),
            end: event.end || undefined,
            extendedProps: (event.extendedProps || {}) as {
              description?: string;
              location?: string;
              organizer?: string;
              schedules?: { title: string }[];
            }
          }
        });
      }
    }
    setIsOpenEventModal(true);
  };

  useEffect(() => {
    if (calendarRef.current) {
      setCalendarApi(calendarRef.current.getApi());
    }
  }, []);

  const getViewTitle = () => {
    if (!calendarApi) return '';
    return calendarApi.view?.title || '';
  };

  const getDate = () => {
    if (!calendarApi) return '';
    const currentDate = calendarApi.getDate();
    if (!currentDate) return '';
    return currentDate.toLocaleString('en-us', {
      weekday: 'long'
    });
  };

  return (
    <>
      <Card className="overflow-hidden h-100">
        <Card.Body className="p-0 management-calendar">
          <Row className="g-3">
            <Col md={7}>
              <div className="p-x1">
                <Flex justifyContent="between">
                  <div className="order-md-1">
                    <OverlayTrigger
                      overlay={
                        <Tooltip style={{ position: 'fixed' }} id="nextTooltip">
                          Previous
                        </Tooltip>
                      }
                    >
                      <Button
                        variant="falcon-default"
                        size="sm"
                        className="me-1"
                        onClick={() => {
                          if (!calendarApi) return;
                          calendarApi.prev();
                          setTitle(getViewTitle());
                          setDay(getDate());
                        }}
                      >
                        <FontAwesomeIcon icon="chevron-left" />
                      </Button>
                    </OverlayTrigger>
                    <Button
                      size="sm"
                      variant="falcon-default"
                      onClick={() => {
                        if (!calendarApi) return;
                        calendarApi.today();
                        setTitle(getViewTitle());
                        setDay(getDate());
                      }}
                      className="px-sm-4"
                    >
                      Today
                    </Button>
                    <OverlayTrigger
                      overlay={
                        <Tooltip style={{ position: 'fixed' }} id="nextTooltip">
                          Next
                        </Tooltip>
                      }
                    >
                      <Button
                        variant="falcon-default"
                        size="sm"
                        className="ms-1"
                        onClick={() => {
                          if (!calendarApi) return;
                          calendarApi.next();
                          setTitle(getViewTitle());
                          setDay(getDate());
                        }}
                      >
                        <FontAwesomeIcon icon="chevron-right" />
                      </Button>
                    </OverlayTrigger>
                  </div>

                  <IconButton
                    variant="falcon-primary"
                    iconClassName="me-2"
                    icon="plus"
                    size="sm"
                    onClick={() => {
                      setIsOpenScheduleModal(!isOpenScheduleModal);
                    }}
                  >
                    New <span className="d-none d-sm-inline">Schedule</span>
                  </IconButton>
                </Flex>
              </div>
              <div className="calendar-outline px-3">
                <FullCalendar
                  ref={calendarRef}
                  headerToolbar={false}
                  plugins={[dayGridPlugin, timeGridPlugin, interactionPlugin]}
                  themeSystem="bootstrap"
                  direction={isRTL ? 'rtl' : 'ltr'}
                  height={360}
                  dateClick={info => {
                    setIsOpenScheduleModal(true);
                    setScheduleStartDate(info.date);
                  }}
                  events={data.map(e => ({ ...e, id: String(e.id) }))}
                />
              </div>
            </Col>
            <Col md={5} className="bg-body-tertiary pt-3">
              <div className="px-3">
                <h4 className="mb-0 fs-9 fs-sm-8 fs-lg-7">
                  {title || getViewTitle()}
                </h4>
                <p className="text-500 mb-0">
                  {day ||
                    `${new Date().toLocaleString('en-us', {
                      weekday: 'long'
                    })}`}
                </p>
                <ul
                  className="list-unstyled mt-3 scrollbar management-calendar-events"
                  id="management-calendar-events"
                >
                  {data.map(events => (
                    <li
                      className="border-top pt-3 mb-3 pb-1 cursor-pointer"
                      onClick={() => handleEventClick(events)}
                      key={events.id}
                    >
                      <div
                        className={`border-start border-3 ps-3 mt-1 border-${events.color}`}
                      >
                        <h6 className="mb-1 fw-semibold text-700">
                          {events.title}
                        </h6>
                        <p className="fs-11 text-600 mb-0">
                          {events.startTime || ''} {events.endTime ? '- ' : ''}
                          {events.endTime || ''}
                        </p>
                      </div>
                    </li>
                  ))}
                </ul>
              </div>
            </Col>
          </Row>
        </Card.Body>
      </Card>

      <AddScheduleModal
        isOpenScheduleModal={isOpenScheduleModal}
        setIsOpenScheduleModal={setIsOpenScheduleModal}
        scheduleStartDate={scheduleStartDate}
        scheduleEndDate={scheduleEndDate}
        setScheduleStartDate={setScheduleStartDate}
        setScheduleEndDate={setScheduleEndDate}
      />

      <CalendarEventModal
        isOpenEventModal={isOpenEventModal}
        setIsOpenEventModal={setIsOpenEventModal}
        modalEventContent={modalEventContent}
      />
    </>
  );
};

export default CalendarManagement;
