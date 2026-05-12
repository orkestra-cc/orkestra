import { useRef, MouseEvent } from 'react';
import { Card, Col, Row, Button } from 'react-bootstrap';
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome';
import ReportForThisWeekChart from './ReportForThisWeekChart';
import OrkestraCardFooterLink from 'components/common/OrkestraCardFooterLink';
import Flex from 'components/common/Flex';
import ReactEChartsCore from 'echarts-for-react/lib/core';

interface ReportData {
  thisWeek: number[];
  lastWeek: number[];
}

interface ReportForThisWeekProps {
  data: ReportData;
}

const ReportForThisWeek = ({ data }: ReportForThisWeekProps) => {
  const chartRef = useRef<ReactEChartsCore | null>(null);
  const handleLegend = (event: MouseEvent<HTMLButtonElement>, name: string) => {
    if (chartRef.current) {
      chartRef.current.getEchartsInstance().dispatchAction({
        type: 'legendToggleSelect',
        name: name
      });
    }
    (event.target as HTMLElement)
      .closest('button')
      ?.classList.toggle('opacity-50');
  };
  return (
    <Card>
      <Card.Header className="pb-0">
        <Row className="flex-between-center gap-2">
          <Col xs="auto">
            <h6 className="mb-0">Report for this week</h6>
          </Col>
          <Col xs="auto" as={Flex}>
            <Button
              size="sm"
              className="d-flex align-items-center p-0  me-3 shadow-none"
              variant="default"
              onClick={event => handleLegend(event, 'This Week')}
            >
              <FontAwesomeIcon
                icon="circle"
                className="text-primary fs-11 me-1"
              />
              This Week
            </Button>
            <Button
              size="sm"
              className="d-flex align-items-center p-0 shadow-none"
              variant="default"
              onClick={event => handleLegend(event, 'Last Week')}
            >
              <FontAwesomeIcon icon="circle" className="text-300 fs-11 me-1" />
              Last Week
            </Button>
          </Col>
        </Row>
      </Card.Header>
      <Card.Body className="py-0">
        <ReportForThisWeekChart data={data} ref={chartRef} />
      </Card.Body>
      <OrkestraCardFooterLink title="See all projects" size="sm" />
    </Card>
  );
};

export default ReportForThisWeek;
