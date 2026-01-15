import { Col, Form, Row, Button } from 'react-bootstrap';
import DatePicker from 'react-datepicker';
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome';
import CustomDateInput from 'components/common/CustomDateInput';
import { UseFormRegister, UseFormSetValue, FieldValues } from 'react-hook-form';
import { forwardRef } from 'react';

// Pre-create a properly typed forwardRef component for DatePicker customInput
const DateInputWrapper = forwardRef<HTMLInputElement, any>((props, ref) => (
  <CustomDateInput {...props} ref={ref} />
));

interface EventScheduleItemProps {
  index: number;
  title: string;
  handleRemove: (index: number) => void;
  handleChange: (id: number, name: string, value: string | Date | null) => void;
  startDate: Date | null;
  startTime: Date | null;
  endDate: Date | null;
  endTime: Date | null;
  register: UseFormRegister<FieldValues>;
  setValue: UseFormSetValue<FieldValues>;
}

const EventScheduleItem = ({
  index,
  title,
  handleRemove,
  handleChange,
  startDate,
  startTime,
  endDate,
  endTime,
  register,
  setValue
}: EventScheduleItemProps) => {
  return (
    <div className="bg-body-emphasis border p-3 position-relative rounded-1 mb-2">
      <div className="position-absolute end-0 top-0 mt-2 me-3 z-1">
        <Button
          size="sm"
          variant="link"
          className="p-0"
          onClick={() => handleRemove(index)}
        >
          <FontAwesomeIcon className="text-danger" icon="times-circle" />
        </Button>
      </div>

      <Row className="gx-2 gy-3">
        <Col md="12">
          <Form.Group controlId="scheduleStartTitle">
            <Form.Label>Title</Form.Label>
            <Form.Control
              type="text"
              placeholder="Title"
              defaultValue={title}
              {...register(`scheduleTitle${index}`)}
            />
          </Form.Group>
        </Col>
        <Col md="6">
          <Form.Group controlId={`ScheduleStartDate${index}`}>
            <Form.Label>Start Date</Form.Label>

            <DatePicker
              selected={startDate}
              onChange={(newDate: Date | null) => {
                handleChange(index, 'startDate', newDate);
                setValue(`ScheduleStartDate${index}`, newDate);
              }}
              customInput={<DateInputWrapper formControlProps={{ placeholder: 'd/m/y', ...register(`ScheduleStartDate${index}`) }} />}
            />
          </Form.Group>
        </Col>

        <Col md="6">
          <Form.Group controlId="scheduleStartTime">
            <Form.Label>Start Time</Form.Label>
            <DatePicker
              selected={startTime}
              showTimeSelect
              showTimeSelectOnly
              timeIntervals={15}
              timeCaption="Time"
              dateFormat="h:mm"
              onChange={(newDate: Date | null) => {
                handleChange(index, 'startTime', newDate);
                setValue(`ScheduleStartTime${index}`, newDate);
              }}
              customInput={<DateInputWrapper formControlProps={{ placeholder: 'H:i', ...register(`ScheduleStartTime${index}`) }} />}
            />
          </Form.Group>
        </Col>
        <Col md="6">
          <Form.Group controlId="scheduleEndDate">
            <Form.Label>End Date</Form.Label>

            <DatePicker
              selected={endDate}
              onChange={(newDate: Date | null) => {
                handleChange(index, 'endDate', newDate);
                setValue(`ScheduleEndDate${index}`, newDate);
              }}
              customInput={<DateInputWrapper formControlProps={{ placeholder: 'd/m/y', ...register(`ScheduleEndDate${index}`) }} />}
            />
          </Form.Group>
        </Col>
        <Col md="6">
          <Form.Group controlId="scheduleEndTime">
            <Form.Label>End Time</Form.Label>
            <DatePicker
              selected={endTime}
              showTimeSelect
              showTimeSelectOnly
              timeIntervals={15}
              timeCaption="Time"
              dateFormat="h:mm"
              onChange={(newDate: Date | null) => {
                handleChange(index, 'endTime', newDate);
                setValue(`ScheduleEndTime${index}`, newDate);
              }}
              customInput={<DateInputWrapper formControlProps={{ placeholder: 'H:i', ...register(`ScheduleEndTime${index}`) }} />}
            />
          </Form.Group>
        </Col>
      </Row>
    </div>
  );
};
export default EventScheduleItem;