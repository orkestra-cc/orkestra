
import { Button, Row, Col, ListGroup } from 'react-bootstrap';
import PageHeader from 'components/common/PageHeader';
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome';
import FalconComponentCard from 'components/common/FalconComponentCard';
import DatePicker, { registerLocale } from 'react-datepicker';
import { es } from 'date-fns/locale/es';
import FalconEditor from 'components/common/FalconEditor';

registerLocale('es', es);

const timeCode = `function DatepickerExample() {
  const [date, setDate] = useState(null);
  
  return (
    <DatePicker
      popperPlacement='bottom-start'
      selected={date}
      onChange={(date)=>setDate(date)}
      className='form-control'
      placeholderText="Select Time"
      timeIntervals={5}
      dateFormat="h:mm aa"
      showTimeSelect
      showTimeSelectOnly
    />
  );
}`;

const dateCode = `function DatePickerExample() {
  const [date, setDate] = useState(null);
  
  return (

    <DatePicker
      popperPlacement='bottom-start'
      selected={date}
      onChange={(date)=>setDate(date)}
      formatWeekDay={day => day.slice(0, 3)}
      className='form-control'
      placeholderText="Select Date"
    />
  );
}`;

const dateTimeCode = `function DatePickerExample() {
  const [date, setDate] = useState(null);
  
  return (
    <DatePicker
    popperPlacement='bottom-start'
    selected={date}
    onChange={(date)=>setDate(date)}
    formatWeekDay={day => day.slice(0, 3)}
    className='form-control'
    placeholderText="Select Date & Time"
    timeIntervals={5}
    dateFormat="MM/dd/yyyy h:mm aa"
    showTimeSelect
    fixedHeight
  />
  );
}`;

const formatCode = `function DatepickerExample() {
  const [date, setDate] = useState(null);
  
  return (
    <DatePicker
      popperPlacement='bottom-start'
      selected={date}
      onChange={(date)=>setDate(date)}
      className='form-control'
      placeholderText="DD-MM-YYYY hh:mm aa"
      timeIntervals={5}
      dateFormat="dd-MM-yyyy hh:mm aa"
      showTimeSelect
      fixedHeight
    />
  );
}`;

const rangeCode = `function DatepickerExample() {
  const date = new Date();
  const [startDate, setStartDate] = useState(new Date());
  const [endDate, setEndDate] = useState(date.setDate(date.getDate() + 7));
  const onChange = dates => {
    const [start, end] = dates;
    setStartDate(start);
    setEndDate(end);
  };
  return (
    <DatePicker
      popperPlacement='bottom-start'
      selected={startDate}
      onChange={onChange}
      startDate={startDate}
      formatWeekDay={day => day.slice(0, 3)}
      endDate={endDate}
      selectsRange
      dateFormat="MMM dd"
      className='form-control'
    />
  );
}`;

const predefinedRangesCode = `function DatepickerExample() {
  const date = new Date();
  const [startDate, setStartDate] = useState(new Date());
  const [endDate, setEndDate] = useState(date.setDate(date.getDate() + 7));
  const onChange = dates => {
    const [start, end] = dates;
    setStartDate(start);
    setEndDate(end);
  };
  const handleLastMonth = () => {
    const today = new Date();
    const lastMonth = new Date(today.getFullYear(), today.getMonth() - 1, 1);
    const endOfLastMonth = new Date(today.getFullYear(), today.getMonth(), 0);

    setStartDate(lastMonth);
    setEndDate(endOfLastMonth);
  };
  const handleThisMonth = () => {
    const today = new Date();
    const startOfMonth = new Date(today.getFullYear(), today.getMonth(), 1);
    const endOfMonth = new Date(today.getFullYear(), today.getMonth() + 1, 0);

    setStartDate(startOfMonth);
    setEndDate(endOfMonth);
  };
  const handleLastWeek = () => {
    const today = new Date();
    const lastSunday = new Date(today.getFullYear(), today.getMonth(), today.getDate() - today.getDay() - 7);
    const lastSaturday = new Date(today.getFullYear(), today.getMonth(), today.getDate() - today.getDay() - 1);
  
    setStartDate(lastSunday);
    setEndDate(lastSaturday);
  };
  return (
    <DatePicker
      popperPlacement='bottom-start'
      selected={startDate}
      onChange={onChange}
      startDate={startDate}
      formatWeekDay={day => day.slice(0, 3)}
      calendarContainer={({ children }) => (
        <div className="react-datepicker react-datepicker__predefined-ranges">
          <ListGroup variant="flush" className="react-datepicker__ranges">
            <ListGroup.Item action onClick={handleLastWeek}>
              Last Week
            </ListGroup.Item>
            <ListGroup.Item action onClick={handleLastMonth}>
              Last Month
            </ListGroup.Item>
            <ListGroup.Item action onClick={handleThisMonth}>
              This Month
            </ListGroup.Item>
          </ListGroup>
          {children}
        </div>
      )}
      showPreviousMonths
      endDate={endDate}
      selectsRange
      dateFormat="MMM dd"
      className='form-control'
    />
  );
}`;

const customInputCode = `function DatepickerExample() {
  const [startDate, setStartDate] = useState(new Date());
  const ExampleCustomInput = ({ value, onClick, ref }) => (
    <div className="position-relative">
      <Form.Control
        ref={ref}
        onClick={onClick}
        value={value}
        className="ps-4"
        onChange={()=>{}}
      />
      <FontAwesomeIcon
        icon="calendar-alt"
        className="text-primary position-absolute top-50 translate-middle-y ms-2"
      />
    </div>
  );
  return (
    <DatePicker
      popperPlacement='bottom-start'
      selected={startDate}
      onChange={(date) => setStartDate(date)}
      customInput={<ExampleCustomInput />}
    />
  );
}`;

const localeDocCode = `import { registerLocale, setDefaultLocale } from  "react-datepicker";
import es from 'date-fns/locale/es';
registerLocale('es', es)

  <DatePicker
    popperPlacement='bottom-start'
    locale="es"
  />
`;

const LocalizationCode = `function DatepickerExample() {
  const [date, setDate] = useState(null);
  
  return (
    <DatePicker
      popperPlacement='bottom-start'
      selected={date}
      onChange={(date)=>setDate(date)}
      className='form-control'
      placeholderText="Select Date"
      dateFormat="dd-MM-yyyy"
      locale='es'
    />
  );
}`;

const DatePickerExample = () => (
  <>
    <PageHeader
      title="Date Picker"
      description="Falcon-React uses <strong>React Datepicker</strong> as datepicker. <strong>React Datepicker</strong> is a simple and reusable Datepicker component for React."
      className="mb-3"
    >
      <Button
        href="https://github.com/Hacker0x01/react-datepicker"
        target="_blank" rel="noopener noreferrer"
        variant="link"
        size="sm"
        className="ps-0"
      >
        React-Datepicker Documentation
        <FontAwesomeIcon icon="chevron-right" className="ms-1 fs-11" />
      </Button>
    </PageHeader>

    <Row className="mb-3 g-3">
      <Col lg={6}>
        <FalconComponentCard noGuttersBottom>
          <FalconComponentCard.Header title="Date picker" />
          <FalconComponentCard.Body
            code={dateCode}
            scope={{ DatePicker }}
            language="jsx"
          />
        </FalconComponentCard>
      </Col>
      <Col lg={6}>
        <FalconComponentCard noGuttersBottom>
          <FalconComponentCard.Header title="Time picker" />

          <FalconComponentCard.Body
            code={timeCode}
            scope={{ DatePicker }}
            language="jsx"
          />
        </FalconComponentCard>
      </Col>
    </Row>

    <Row className="mb-3 g-3">
      <Col lg={6}>
        <FalconComponentCard noGuttersBottom>
          <FalconComponentCard.Header title="Date & Time picker" />
          <FalconComponentCard.Body
            code={dateTimeCode}
            scope={{ DatePicker }}
            language="jsx"
          />
        </FalconComponentCard>
      </Col>
      <Col lg={6}>
        <FalconComponentCard noGuttersBottom>
          <FalconComponentCard.Header title="Date Format" />
          <FalconComponentCard.Body
            code={formatCode}
            scope={{ DatePicker }}
            language="jsx"
          />
        </FalconComponentCard>
      </Col>
    </Row>

    <Row className="mb-3 g-3">
      <Col lg={6}>
        <FalconComponentCard noGuttersBottom>
          <FalconComponentCard.Header title="Date Range" />
          <FalconComponentCard.Body
            code={rangeCode}
            scope={{ DatePicker }}
            language="jsx"
          />
        </FalconComponentCard>
      </Col>
      <Col lg={6}>
        <FalconComponentCard noGuttersBottom>
          <FalconComponentCard.Header title="Custom Input" />
          <FalconComponentCard.Body
            code={customInputCode}
            scope={{ DatePicker, FontAwesomeIcon }}
            language="jsx"
          />
        </FalconComponentCard>
      </Col>
    </Row>
    <Row className="mb-3">
      <Col>
        <FalconComponentCard noGuttersBottom>
          <FalconComponentCard.Header title="Predefined Date Range" />
          <FalconComponentCard.Body
            code={predefinedRangesCode}
            scope={{ DatePicker, ListGroup }}
            language="jsx"
          />
        </FalconComponentCard>
      </Col>
    </Row>

    <FalconComponentCard>
      <FalconComponentCard.Header title="Localization" noPreview />
      <FalconComponentCard.Body>
        <p className="mb-1">
          To localize <strong>React Datepicker</strong> display components
          import <code>registerLocale</code> from react-datepicker and{' '}
          <code>locale</code> object from date-fns.Then use{' '}
          <code>registerLocale</code> to loads the imported locale object from
          date-fns.For more details visit{' '}
          <a href="https://github.com/Hacker0x01/react-datepicker#localization">
            React Datepicker Localization Documentation.
          </a>
        </p>
        <FalconEditor code={localeDocCode} language="js" hidePreview />
      </FalconComponentCard.Body>
    </FalconComponentCard>

    <FalconComponentCard noGuttersBottom>
      <FalconComponentCard.Header title="Localization Example" />
      <FalconComponentCard.Body
        code={LocalizationCode}
        scope={{ DatePicker }}
      />
    </FalconComponentCard>
  </>
);

export default DatePickerExample;
