import { useRef, ChangeEvent } from 'react';
import classNames from 'classnames';
import { Badge, Card, Col, Form, Row } from 'react-bootstrap';
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome';
import NumberOfTicketsChart from './NumberOfTicketsChart';
import { IconProp } from '@fortawesome/fontawesome-svg-core';
import ReactEChartsCore from 'echarts-for-react/lib/core';

interface FormCheckProps {
  title: string;
  id: string;
  inputClass?: string;
  checkBoxClass?: string;
  handleLegend: (event: ChangeEvent<HTMLInputElement>, name: string) => void;
}

interface TicketsCategoryProps {
  title: string;
  number: string;
  percentage: string;
  icon: IconProp;
  badgeColor: string;
}

interface NumberOfTicketsProps {
  data: number[][];
}

const FormCheck = ({ title, id, inputClass, checkBoxClass, handleLegend }: FormCheckProps) => {
  return (
    <Form.Check
      className={classNames(checkBoxClass, 'd-flex align-items-center mb-0')}
    >
      <Form.Check.Input
        onChange={event => handleLegend(event, title)}
        type="checkbox"
        id={id}
        defaultChecked
        className={classNames(
          inputClass,
          'dot mt-0 shadow-none remove-checked-icon rounded-circle cursor-pointer'
        )}
      />
      <Form.Check.Label
        htmlFor={id}
        className="form-check-label lh-base mb-0 fs-11 text-500 fw-semibold font-base cursor-pointer"
      >
        {title}
      </Form.Check.Label>
    </Form.Check>
  );
};

const TicketsCategory = ({ title, number, percentage, icon, badgeColor }: TicketsCategoryProps) => {
  return (
    <div>
      <h6 className="fs-9 d-flex align-items-center text-700 mb-1">
        {number}
        <Badge pill bg="transparent" className={classNames(badgeColor, 'px-0')}>
          <FontAwesomeIcon icon={icon} className="fs-11 ms-2 me-1" />
          {percentage}
        </Badge>
      </h6>
      <h6 className="fs-11 text-600 mb-0 text-nowrap">{title}</h6>
    </div>
  );
};

const NumberOfTickets = ({ data }: NumberOfTicketsProps) => {
  const chartRef = useRef<ReactEChartsCore | null>(null);
  const handleLegend = (_event: ChangeEvent<HTMLInputElement>, name: string) => {
    if (chartRef.current) {
      chartRef.current.getEchartsInstance().dispatchAction({
        type: 'legendToggleSelect',
        name: name
      });
    }
  };
  return (
    <Card className="h-100">
      <Card.Header className="d-md-flex justify-content-between border-bottom border-200 py-3 py-md-2">
        <h6 className="mb-2 mb-md-0 py-md-2">Number of Tickets</h6>
        <Row className="g-md-0">
          <Col xs="auto" className="d-md-flex">
            <FormCheck
              title="On Hold Tickets"
              id="onHoldTickets"
              checkBoxClass="me-md-3"
              handleLegend={handleLegend}
            />
            <FormCheck
              title="Open Tickets"
              id="openTickets"
              inputClass="open-tickets"
              checkBoxClass="mt-n1 mt-md-0 me-md-3"
              handleLegend={handleLegend}
            />
          </Col>
          <Col xs="auto" className="d-md-flex">
            <FormCheck
              title="Due Tickets"
              id="dueTickets"
              inputClass="due-tickets"
              checkBoxClass="me-md-3"
              handleLegend={handleLegend}
            />
            <FormCheck
              title="Unassigned Tickets"
              id="unassignedTickets"
              inputClass="unassigned-tickets"
              checkBoxClass="mt-n1 mt-md-0 me-md-0"
              handleLegend={handleLegend}
            />
          </Col>
        </Row>
      </Card.Header>
      <Card.Body className="scrollbar overflow-y-hidden">
        <div className="d-flex">
          <div className="d-flex align-items-center">
            <TicketsCategory
              title="Total On Hold Tickets"
              number="125"
              percentage="5.3%"
              icon="caret-up"
              badgeColor="text-success"
            />
            <div
              className="bg-200 mx-3"
              style={{ height: '24px', width: '1px' }}
            ></div>
          </div>
          <div className="d-flex align-items-center">
            <TicketsCategory
              title="Total Open Tickets"
              number="100"
              percentage="3.20%"
              icon="caret-up"
              badgeColor="text-primary"
            />
            <div
              className="bg-200 mx-3"
              style={{ height: '24px', width: '1px' }}
            ></div>
          </div>
          <div className="d-flex align-items-center">
            <TicketsCategory
              title="Total Due Tickets"
              number="53"
              percentage="2.3%"
              icon="caret-down"
              badgeColor="text-warning"
            />
            <div
              className="bg-200 mx-3"
              style={{ height: '24px', width: '1px' }}
            ></div>
          </div>
          <TicketsCategory
            title="Total Unassigned Tickets"
            number="136"
            percentage="3.12%"
            icon="caret-up"
            badgeColor="text-danger"
          />
        </div>
        <NumberOfTicketsChart data={data} ref={chartRef} />
      </Card.Body>
      <Card.Footer className="bg-body-tertiary py-2">
        <Row className="flex-between-center g-0">
          <Col xs="auto">
            <Form.Select defaultValue="March" size="sm">
              <option>January</option>
              <option>February</option>
              <option>March</option>
              <option>April</option>
              <option>May</option>
              <option>Jun</option>
              <option>July</option>
              <option>September</option>
              <option>October</option>
              <option>November</option>
              <option>December</option>
            </Form.Select>
          </Col>
          <Col xs="auto">
            <a href="#!" className="btn btn-link btn-sm px-0 fw-medium">
              View all reports
              <FontAwesomeIcon icon="chevron-right" className="ms-1 fs-11" />
            </a>
          </Col>
        </Row>
      </Card.Footer>
    </Card>
  );
};

export default NumberOfTickets;
