import { useRef, useState } from 'react';
import { Card, Col, Row, ButtonGroup, Button } from 'react-bootstrap';
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome';
import Stat from './Stat';
import CandleChartBody from './CandleChartBody';
import { candleChartStatsData } from 'data/dashboard/saas';
import ReactEChartsCore from 'echarts-for-react/lib/core';
import { BadgeColor } from 'components/common/SubtleBadge';

// Raw data format: [date, open, close, low, high]
type RawDataItem = [string, number, number, number, number];

interface GrowInfo {
  color: BadgeColor;
  isGrow: boolean;
  growAmount: number;
}

interface StatInfo {
  id: string | number;
  title: string;
  grow: GrowInfo;
  amount: string;
}

interface CandleChartProps {
  data: RawDataItem[];
}

const CandleChart = ({ data }: CandleChartProps) => {
  const chartRef = useRef<ReactEChartsCore>(null);
  const [zoomStart] = useState(0);
  const [zoomEnd, setZoomEnd] = useState(70);

  const zoomInAction = () => {
    zoomEnd > 10 && setZoomEnd(zoomEnd - 10);
  };

  const zoomOutAction = () => {
    zoomEnd < 100 && setZoomEnd(zoomEnd + 10);
  };

  return (
    <Card className="h-100" dir="ltr">
      <Card.Header>
        <Row className="flex-between-center">
          <Col xs="auto">
            <h6 className="mb-2">Candle Chart</h6>
          </Col>
          <Col xs="auto" className="mt-2">
            <Row className="g-sm-4">
              {candleChartStatsData.map((stat: StatInfo, index: number) => (
                <Stat
                  key={stat.id}
                  statInfo={stat}
                  isLast={index === candleChartStatsData.length - 1}
                />
              ))}
            </Row>
          </Col>
        </Row>
      </Card.Header>
      <Card.Body className="pe-0 position-relative">
        <ButtonGroup
          className="position-absolute z-1 top-0 d-inline-block"
          role="group"
          style={{ left: '20px', right: '20px' }}
          dir="ltr"
        >
          <Button
            variant="falcon-default"
            size="sm"
            className="mb-1"
            onClick={zoomInAction}
            disabled={zoomEnd <= 10}
          >
            <FontAwesomeIcon icon="plus" />
          </Button>
          <Button
            variant="falcon-default"
            size="sm"
            className="mb-1"
            onClick={zoomOutAction}
            disabled={zoomEnd >= 100}
          >
            <FontAwesomeIcon icon="minus" />
          </Button>
        </ButtonGroup>
        <CandleChartBody
          ref={chartRef}
          zoomStart={zoomStart}
          zoomEnd={zoomEnd}
          data={data}
          style={{ height: '20.5rem' }}
        />
      </Card.Body>
    </Card>
  );
};

export default CandleChart;
