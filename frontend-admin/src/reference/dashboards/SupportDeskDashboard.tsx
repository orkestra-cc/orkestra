
import { Col, Row } from 'react-bootstrap';
import {
  statusData,
  unresolvedTickets,
  numbersOfTickets
} from 'data/dashboard/support-desk';
import Greetings from 'components/dashboards/support-desk/Greetings';
import TicketStatus from 'components/dashboards/support-desk/TicketStatus';
import UnresolvedTickets from 'components/dashboards/support-desk/unresolved-tickets/UnresolvedTickets';
import NumberOfTickets from 'components/dashboards/support-desk/number-of-tickets/NumberOfTickets';
import CustomerSatisfaction from 'components/dashboards/support-desk/customer-satisfaction/CustomerSatisfaction';
import ToDoList from 'components/dashboards/support-desk/ToDoList';
import UnsolvedTickets from 'components/dashboards/support-desk/unsolved-tickets/UnsolvedTickets';

const SupportDesk: React.FC = () => {
  return (
    <>
      <Row className="g-3 mb-3">
        <Col xxl={6}>
          <Row className="g-0 h-100">
            <Col xs={12} className="mb-3">
              <Greetings />
            </Col>
            <Col>
              <TicketStatus data={statusData} />
            </Col>
          </Row>
        </Col>
        <Col xxl={6}>
          <UnresolvedTickets data={unresolvedTickets} />
        </Col>
      </Row>
      <Row className="g-3">
        <Col xxl={8}>
          <NumberOfTickets data={numbersOfTickets} />
        </Col>
        <Col md={6} xxl={4}>
          <CustomerSatisfaction />
        </Col>
        <Col md={6} xxl={3}>
          <ToDoList />
        </Col>
        <Col xxl={9}>
          <UnsolvedTickets />
        </Col>
      </Row>
    </>
  );
};

export default SupportDesk;
