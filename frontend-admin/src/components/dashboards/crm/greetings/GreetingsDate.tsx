import { useState, forwardRef, MouseEventHandler } from 'react';
import { Form } from 'react-bootstrap';
import DatePicker from 'react-datepicker';
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome';

interface GreetingsDateInputProps {
  value?: string;
  onClick?: MouseEventHandler<HTMLElement>;
}

const GreetingsDateInput = forwardRef<HTMLInputElement, GreetingsDateInputProps>(
  ({ value, onClick }, ref) => (
  <div className="position-relative">
    <Form.Control
      size="sm"
      ref={ref}
      onClick={onClick}
      value={value}
      className="ps-4"
      onChange={e => {
        console.log({ e });
      }}
    />
    <FontAwesomeIcon
      icon="calendar-alt"
      className="text-primary position-absolute top-50 translate-middle-y ms-2"
    />
  </div>
));

GreetingsDateInput.displayName = 'GreetingsDateInput';

const GreetingsDate = () => {
  const date = new Date();
  const [startDate, setStartDate] = useState<Date | null>(new Date());
  const [endDate, setEndDate] = useState<Date | null>(new Date(date.setDate(date.getDate() + 7)));
  const onChange = (dates: [Date | null, Date | null]) => {
    const [start, end] = dates;
    setStartDate(start);
    setEndDate(end);
  };
  return (
    <DatePicker
      selected={startDate}
      onChange={onChange}
      startDate={startDate}
      formatWeekDay={day => day.slice(0, 3)}
      endDate={endDate}
      selectsRange
      dateFormat="MMM dd"
      customInput={<GreetingsDateInput />}
    />
  );
};

export default GreetingsDate;
