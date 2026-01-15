import { useRef, useState } from 'react';
import { Card, Col, Form, Row } from 'react-bootstrap';
import Flex from 'components/common/Flex';
import LinePaymentChart from './LinePaymentChart';

interface LinePaymentData {
  successful: number[];
  failed: number[];
  all: number[];
  [key: string]: number[];
}

interface LinePaymentProps {
  data: LinePaymentData;
}

const LinePayment = ({ data }: LinePaymentProps) => {
  const chartRef = useRef(null);
  const [paymentStatus, setPaymentStatus] = useState('successful');

  return (
    <Card className="rounded-3 overflow-hidden h-100 shadow-none">
      <Card.Body
        className="bg-line-chart-gradient"
        as={Flex}
        justifyContent="between"
        direction="column"
      >
        <Row className="align-items-center gx-2 gy-0">
          <Col>
            <h4 className="text-white mb-0 text-nowrap">Today $764.39</h4>
            <p className="fs-10 fw-semibold text-white">
              Yesterday <span className="opacity-50">$684.87</span>
            </p>
          </Col>
          <Col xs="auto">
            <Form.Select
              size="sm"
              className="mb-3"
              value={paymentStatus}
              onChange={e => setPaymentStatus(e.target.value)}
            >
              <option value="all">All Payments</option>
              <option value="successful">Successful Payments</option>
              <option value="failed">Failed Payments</option>
            </Form.Select>
          </Col>
        </Row>
        <LinePaymentChart
          ref={chartRef}
          data={data}
          paymentStatus={paymentStatus}
          style={{ height: '200px' }}
        />
      </Card.Body>
    </Card>
  );
};

export default LinePayment;
