import { useState, useEffect, ChangeEvent, FormEvent, Dispatch, SetStateAction } from 'react';
import { Button, Form, Modal } from 'react-bootstrap';
import { v4 as uuid } from 'uuid';
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome';
import { Link } from 'react-router';
import DatePicker from 'react-datepicker';
import { useAppContext } from 'providers/AppProvider';

interface CalendarEvent {
  id: string;
  title?: string;
  start?: Date | null;
  end?: Date | null;
  allDay?: boolean;
  description?: string;
  className?: string;
}

interface FormData {
  title?: string;
  start?: Date | null;
  end?: Date | null;
  allDay?: boolean;
  description?: string;
  className?: string;
}

interface AddScheduleModalProps {
  setIsOpenScheduleModal: Dispatch<SetStateAction<boolean>>;
  isOpenScheduleModal: boolean;
  setInitialEvents?: Dispatch<SetStateAction<CalendarEvent[]>>;
  initialEvents?: CalendarEvent[];
  scheduleStartDate: Date | null | undefined;
  setScheduleStartDate: Dispatch<SetStateAction<Date | null | undefined>>;
  scheduleEndDate: Date | null | undefined;
  setScheduleEndDate: Dispatch<SetStateAction<Date | null | undefined>>;
}

const AddScheduleModal = ({
  setIsOpenScheduleModal,
  isOpenScheduleModal,
  setInitialEvents,
  initialEvents,
  scheduleStartDate,
  setScheduleStartDate,
  scheduleEndDate,
  setScheduleEndDate
}: AddScheduleModalProps) => {
  const {
    config: { isDark }
  } = useAppContext();

  const [formData, setFormData] = useState<FormData>({});

  const handleClose = () => {
    setIsOpenScheduleModal(!isOpenScheduleModal);
  };

  const handleChange = ({ target }: ChangeEvent<HTMLInputElement | HTMLTextAreaElement | HTMLSelectElement>) => {
    const name = target.name;
    const value = name === 'allDay' ? (target as HTMLInputElement).checked : target.value;
    setFormData({ ...formData, [name]: value });
  };
  const handleSubmit = (e: FormEvent<HTMLFormElement>) => {
    e.preventDefault();
    if (setInitialEvents && initialEvents) {
      setInitialEvents([...initialEvents, { id: uuid(), ...formData }]);
    }
    setIsOpenScheduleModal(false);
  };

  useEffect(() => {
    if (isOpenScheduleModal) {
      setFormData({
        ...formData,
        start: scheduleStartDate,
        end: scheduleEndDate
      });
    } else {
      setScheduleStartDate(null);
      setScheduleEndDate(null);
    }
  }, [isOpenScheduleModal, scheduleStartDate, scheduleEndDate]);

  return (
    <Modal
      show={isOpenScheduleModal}
      onHide={handleClose}
      contentClassName="border"
    >
      <Form onSubmit={handleSubmit}>
        <Modal.Header
          closeButton
          closeVariant={isDark ? 'white' : undefined}
          className="bg-body-tertiary px-x1 border-bottom-0"
        >
          <Modal.Title as="h5"> Create Schedule</Modal.Title>
        </Modal.Header>
        <Modal.Body className="p-x1">
          <Form.Group className="mb-3" controlId="titleInput">
            <Form.Label className="fs-9">Title</Form.Label>
            <Form.Control
              type="text"
              name="title"
              required
              onChange={handleChange}
            />
          </Form.Group>
          <Form.Group className="mb-3" controlId="startDate">
            <Form.Label className="fs-9">Start Date</Form.Label>
            <DatePicker
              selected={scheduleStartDate}
              onChange={(date: Date | null) => {
                setScheduleStartDate(date);
                setFormData({ ...formData, start: date });
              }}
              className="form-control"
              placeholderText="MM-DD-YYYY H:M"
              dateFormat="MM-dd-yyyy h:mm aa"
              showTimeSelect
            />
          </Form.Group>
          <Form.Group className="mb-3" controlId="endDate">
            <Form.Label className="fs-9">End Date</Form.Label>
            <DatePicker
              selected={scheduleEndDate}
              onChange={(date: Date | null) => {
                setScheduleEndDate(date);
                setFormData({ ...formData, end: date });
              }}
              className="form-control"
              placeholderText="MM-DD-YYYY H:M"
              dateFormat="MM-dd-yyyy h:mm aa"
              showTimeSelect
            />
          </Form.Group>
          <Form.Group controlId="allDay">
            <Form.Check
              type="checkbox"
              id="allDay"
              label="All Day"
              name="allDay"
              onChange={handleChange}
            />
          </Form.Group>
          <Form.Group className="mb-3">
            <Form.Label className="fs-9">Schedule Meeting</Form.Label>
            <div>
              <Link
                to="#!"
                className="btn btn-link badge-subtle-success fw-medium btn-sm"
              >
                <FontAwesomeIcon icon="video" className="me-2" />
                <span>Add video conference link</span>
              </Link>
            </div>
          </Form.Group>

          <Form.Group className="mb-3" controlId="description">
            <Form.Label className="fs-9">Description</Form.Label>
            <Form.Control
              as="textarea"
              rows={3}
              name="description"
              onChange={handleChange}
            />
          </Form.Group>
          <Form.Group className="mb-3" controlId="label">
            <Form.Label className="fs-9">Label</Form.Label>
            <Form.Select name="className" onChange={handleChange}>
              <option>None</option>
              <option value="bg-info-subtle">Business</option>
              <option value="bg-danger-subtle">Important</option>
              <option value="bg-warning-subtle">Personal</option>
              <option value="bg-success-subtle">Must Attend</option>
            </Form.Select>
          </Form.Group>
        </Modal.Body>
        <Modal.Footer className="bg-body-tertiary px-x1 border-top-0">
          <Link to="#!" className="me-3 text-600">
            More options
          </Link>
          <Button
            variant="primary"
            type="submit"
            onClick={handleClose}
            className="px-4 mx-0"
          >
            Save
          </Button>
        </Modal.Footer>
      </Form>
    </Modal>
  );
};


export default AddScheduleModal;
