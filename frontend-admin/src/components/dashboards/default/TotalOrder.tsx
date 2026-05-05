
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome';
import { Badge, Card } from 'react-bootstrap';
import Flex from 'components/common/Flex';
import { rgbaColor } from 'helpers/utils';
import * as echarts from 'echarts/core';
import { LineChart } from 'echarts/charts';

import {
  GridComponent,
  TooltipComponent,
  TitleComponent
} from 'echarts/components';
import { CanvasRenderer } from 'echarts/renderers';
import BasicECharts from 'components/common/BasicEChart';
import { useAppContext } from 'providers/AppProvider';

// TypeScript interfaces
interface TotalOrderProps {
  data: number[];
}

echarts.use([
  TitleComponent,
  TooltipComponent,
  GridComponent,
  LineChart,
  CanvasRenderer
]);

const getOptions = (getThemeColor: (color: string) => string, data: number[]) => ({
  tooltip: {
    trigger: 'axis',
    formatter: '<strong>{b0}</strong> : {c0}'
  },
  xAxis: {
    data: ['Week 4', 'Week 5', 'week 6', 'week 7']
  },
  series: [
    {
      type: 'line',
      data,
      smooth: true,
      lineStyle: {
        width: 3
      },

      areaStyle: {
        color: {
          type: 'linear',
          x: 0,
          y: 0,
          x2: 0,
          y2: 1,
          colorStops: [
            {
              offset: 0,
              color: rgbaColor(getThemeColor('primary'), 0.25)
            },
            {
              offset: 1,
              color: rgbaColor(getThemeColor('primary'), 0)
            }
          ]
        }
      }
    }
  ],
  grid: {
    bottom: '2%',
    top: '2%',
    right: '0px',
    left: '10px'
  }
});

const TotalOrder: React.FC<TotalOrderProps> = ({ data }) => {
  const { getThemeColor } = useAppContext();
  return (
    <Card className="h-md-100">
      <Card.Header className="pb-0">
        <h6 className="mb-0 mt-2">Total Order</h6>
      </Card.Header>

      <Card.Body
        as={Flex}
        alignItems="end"
        justifyContent="between"
        className="pt-0"
      >
        <div>
          <h2 className="fw-normal text-700 mb-1 lh-1">58.4K</h2>
          <Badge pill bg="200" className="text-primary fs-11">
            <FontAwesomeIcon icon="caret-up" className="me-1" />
            13.6%
          </Badge>
        </div>
        <div className="ps-0 mt-n4">
          <BasicECharts
            echarts={echarts}
            options={getOptions(getThemeColor, data)}
            style={{ width: '8.5rem', height: 90 }}
          />
        </div>
      </Card.Body>
    </Card>
  );
};

export default TotalOrder;
