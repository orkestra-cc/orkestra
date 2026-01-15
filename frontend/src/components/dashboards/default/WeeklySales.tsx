
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome';
import classNames from 'classnames';
import BasicECharts from 'components/common/BasicEChart';
import Flex from 'components/common/Flex';
import SubtleBadge from 'components/common/SubtleBadge';
import { BarChart } from 'echarts/charts';
import {
  GridComponent,
  TitleComponent,
  TooltipComponent
} from 'echarts/components';
import * as echarts from 'echarts/core';
import { CanvasRenderer } from 'echarts/renderers';
import { useAppContext } from 'providers/AppProvider';
import { Card, OverlayTrigger, Tooltip } from 'react-bootstrap';

// TypeScript interfaces
interface WeeklySalesProps {
  data: number[];
  width?: string;
  amountClassName?: string;
}

echarts.use([
  TitleComponent,
  TooltipComponent,
  GridComponent,
  BarChart,
  CanvasRenderer
]);

const getOptions = (getThemeColor: (color: string) => string, data: number[]) => ({
  tooltip: {
    trigger: 'axis',
    padding: [7, 10],
    formatter: '{b0} : {c0}',
    transitionDuration: 0,
    backgroundColor: getThemeColor('gray-100'),
    borderColor: getThemeColor('gray-300'),
    textStyle: { color: getThemeColor('gray-1100') },
    borderWidth: 1
  },
  xAxis: {
    type: 'category',
    data: ['Sun', 'Mon', 'Tue', 'Wed', 'Thu', 'Fri', 'Sat'],
    boundaryGap: false,
    axisLine: { show: false },
    axisLabel: { show: false },
    axisTick: { show: false },
    axisPointer: { type: 'none' }
  },
  yAxis: {
    type: 'value',
    splitLine: { show: false },
    axisLine: { show: false },
    axisLabel: { show: false },
    axisTick: { show: false },
    axisPointer: { type: 'none' }
  },
  series: [
    {
      type: 'bar',
      showBackground: true,
      backgroundStyle: {
        borderRadius: 10
      },
      barWidth: '5px',
      itemStyle: {
        borderRadius: 10,
        color: getThemeColor('primary')
      },
      data,
      z: 10
    }
  ],
  grid: { right: 5, left: 10, top: 0, bottom: 0 }
});

const WeeklySales: React.FC<WeeklySalesProps> = ({ data, width, amountClassName }) => {
  const { getThemeColor } = useAppContext();
  return (
    <Card className="h-md-100">
      <Card.Header className="pb-0">
        <h6 className="mb-0 mt-2">
          Weekly Sales
          <OverlayTrigger
            placement="top"
            overlay={
              <Tooltip style={{ position: 'fixed' }}>
                Calculated according to last week's sales
              </Tooltip>
            }
          >
            <span>
              <FontAwesomeIcon
                icon={['far', 'question-circle']}
                transform="shrink-1"
                className="ms-1 text-400"
                id="weeklySalesTooltip"
              />
            </span>
          </OverlayTrigger>
        </h6>
      </Card.Header>

      <Card.Body as={Flex} alignItems="end" justifyContent="between">
        <div>
          <h2
            className={classNames(
              amountClassName,
              'mb-1 text-700 fw-normal lh-1'
            )}
          >
            $47K
          </h2>
          <SubtleBadge pill bg="success" className="me-2 fs-11">
            +3.5%
          </SubtleBadge>
        </div>
        <BasicECharts
          echarts={echarts}
          options={getOptions(getThemeColor, data)}
          style={{ width: width || '8.5rem', height: 60 }}
        />
      </Card.Body>
    </Card>
  );
};

export default WeeklySales;
