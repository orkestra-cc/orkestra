
import { Col, ProgressBar, Row } from 'react-bootstrap';
import classNames from 'classnames';

interface StatisticsItem {
  id: string | number;
  variant: string;
  amount: number;
}

interface StatisticsProps {
  data: StatisticsItem[];
}

const Statistics = ({ data }: StatisticsProps) => {
  return (
    <>
      <Row className="mb-2">
        <Col xs={6} className="border-end border-200">
          <h4 className="mb-0"> 5,432</h4>
          <p className="fs-10 text-600 mb-0">Total Work Hours</p>
        </Col>
        <Col xs={3} className="border-end text-center border-200">
          <h4 className="fs-9 mb-0">13</h4>
          <p className="fs-10 text-600 mb-0">Projects</p>
        </Col>
        <Col className="text-center">
          <h4 className="fs-9 mb-0">7</h4>
          <p className="fs-10 text-600 mb-0">Ongoing</p>
        </Col>
      </Row>
      <ProgressBar
        className="overflow-visible mt-4 rounded-3"
        style={{ height: '6px' }}
      >
        {data.map((item: StatisticsItem, index: number) => (
          <ProgressBar
            variant={item.variant}
            now={item.amount}
            key={item.id}
            className={classNames('overflow-visible position-relative', {
              'rounded-end rounded-pill': index === 0,
              'rounded-start rounded-pill': index === data.length - 1,
              'border-end border-100 border-2': index !== data.length - 1,
              'rounded-0': index !== data.length - 1 && index !== 0
            })}
            label={
              <span className="mt-n4 text-900 fw-bold"> {item.amount}%</span>
            }
          />
        ))}
      </ProgressBar>
    </>
  );
};

export default Statistics;
