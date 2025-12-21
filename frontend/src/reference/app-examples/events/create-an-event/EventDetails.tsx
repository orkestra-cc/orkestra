import CustomDateInput from 'components/common/CustomDateInput';
import { timezones } from 'data/events/timezones';
import { useState, forwardRef } from 'react';
import { Button, Card, Col, Form, Row } from 'react-bootstrap';
import DatePicker from 'react-datepicker';
import { UseFormRegister, UseFormSetValue, FieldValues } from 'react-hook-form';

// Pre-create a properly typed forwardRef component for DatePicker customInput
const DateInputWrapper = forwardRef<HTMLInputElement, any>((props, ref) => (
  <CustomDateInput {...props} ref={ref} />
));

interface EventDetailsProps {
  register: UseFormRegister<FieldValues>;
  setValue: UseFormSetValue<FieldValues>;
}

interface FormDataState {
  startDate: Date | null;
  endDate: Date | null;
  regDate: Date | null;
  startTime: Date | null;
  endTime: Date | null;
}

interface TimezoneItem {
  offset: string;
  name: string;
}

const EventDetails = ({ register, setValue }: EventDetailsProps) => {
  const [formData, setFormData] = useState<FormDataState>({
    startDate: null,
    endDate: null,
    regDate: null,
    startTime: null,
    endTime: null
  });

  const handleChange = (name: string, value: Date | null) => {
    setFormData({
      ...formData,
      [name]: value
    });
  };

  return (
    <Card className="mb-3">
      <Card.Header as="h5">Event Details</Card.Header>
      <Card.Body className="bg-body-tertiary">
        <Row className="gx-2 gy-3">
          <Col md="12">
            <Form.Group controlId="eventTitle">
              <Form.Label>Event Title</Form.Label>
              <Form.Control
                type="text"
                placeholder="Event Title"
                {...register('eventTitle')}
              />
            </Form.Group>
          </Col>
          <Col md="6">
            <Form.Group controlId="startDate">
              <Form.Label>Start Date</Form.Label>
              <DatePicker
                selected={formData.startDate}
                onChange={newDate => {
                  handleChange('startDate', newDate);
                  setValue('startDate', newDate);
                }}
                customInput={<DateInputWrapper formControlProps={{ placeholder: 'dd/mm/yyyy', ...register('startDate') }} />}
              />
            </Form.Group>
          </Col>
          <Col md="6">
            <Form.Group controlId="startTime">
              <Form.Label>Start Time</Form.Label>
              <DatePicker
                selected={formData.startTime}
                showTimeSelect
                showTimeSelectOnly
                timeIntervals={15}
                timeCaption="Time"
                dateFormat="h:mm"
                onChange={newDate => {
                  handleChange('startTime', newDate);
                  setValue('startTime', newDate);
                }}
                customInput={<DateInputWrapper formControlProps={{ placeholder: 'H:i', ...register('startTime') }} />}
              />
            </Form.Group>
          </Col>
          <Col md="6">
            <Form.Group controlId="endDate">
              <Form.Label>End Date</Form.Label>

              <DatePicker
                selected={formData.endDate}
                onChange={newDate => {
                  handleChange('endDate', newDate);
                  setValue('endDate', newDate);
                }}
                customInput={<DateInputWrapper formControlProps={{ placeholder: 'dd/mm/yyyy', ...register('endDate') }} />}
              />
            </Form.Group>
          </Col>
          <Col md="6">
            <Form.Group controlId="endTime">
              <Form.Label>End Time</Form.Label>

              <DatePicker
                selected={formData.endTime}
                showTimeSelect
                showTimeSelectOnly
                timeIntervals={15}
                timeCaption="Time"
                dateFormat="h:mm"
                onChange={newDate => {
                  handleChange('endTime', newDate);
                  setValue('endTime', newDate);
                }}
                customInput={<DateInputWrapper formControlProps={{ placeholder: 'H:i', ...register('endTime') }} />}
              />
            </Form.Group>
          </Col>
          <Col md="6">
            <Form.Group controlId="registration">
              <Form.Label>Registration Deadline</Form.Label>
              <DatePicker
                selected={formData.regDate}
                onChange={newDate => {
                  handleChange('regDate', newDate);
                  setValue('regDate', newDate);
                }}
                customInput={<DateInputWrapper formControlProps={{ placeholder: 'dd/mm/yyyy', ...register('regDate') }} />}
              />
            </Form.Group>
          </Col>
          <Col md="6">
            <Form.Group controlId="timezone">
              <Form.Label>Timezone</Form.Label>
              <Form.Select
                aria-label="Default select example"
                {...register('timeZone')}
              >
                {timezones.map((item: TimezoneItem) => (
                  <option
                    value={`${item.offset}/${item.name}`}
                    key={`${item.offset}/${item.name}`}
                  >
                    {`${item.offset}/${item.name}`}
                  </option>
                ))}
              </Form.Select>
            </Form.Group>
          </Col>

          <Col md="12">
            <div className="border-dashed border-bottom"></div>
          </Col>
          <Col md="6">
            <Form.Group controlId="venue">
              <Form.Label>Venue</Form.Label>
              <Form.Control
                type="text"
                placeholder="Venue"
                {...register('venue')}
              />
              <Button size="sm" variant="link" className="p-0">
                Online Event
              </Button>
            </Form.Group>
          </Col>
          <Col md="6">
            <Form.Group controlId="address">
              <Form.Label>Address</Form.Label>
              <Form.Control
                type="text"
                placeholder="Address"
                {...register('address')}
              />
            </Form.Group>
          </Col>
          <Col md="4">
            <Form.Group controlId="city">
              <Form.Label>City</Form.Label>
              <Form.Control
                type="text"
                placeholder="City"
                {...register('city')}
              />
            </Form.Group>
          </Col>
          <Col md="4">
            <Form.Group controlId="state">
              <Form.Label>State</Form.Label>
              <Form.Control
                type="text"
                placeholder="State"
                {...register('state')}
              />
            </Form.Group>
          </Col>
          <Col md="4">
            <Form.Group controlId="country">
              <Form.Label>Country</Form.Label>
              <Form.Control
                type="text"
                placeholder="Country"
                {...register('country')}
              />
            </Form.Group>
          </Col>
          <Col md="12">
            <Form.Group controlId="description">
              <Form.Label>Description</Form.Label>
              <Form.Control
                as="textarea"
                rows={6}
                {...register('description')}
              />
            </Form.Group>
          </Col>
        </Row>
      </Card.Body>
    </Card>
  );
};

export default EventDetails;
