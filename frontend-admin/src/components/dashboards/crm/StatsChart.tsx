
import * as echarts from 'echarts/core';
import { LineChart } from 'echarts/charts';
import {
  GridComponent,
  TooltipComponent,
  TitleComponent
} from 'echarts/components';
import { CanvasRenderer } from 'echarts/renderers';
import BasicECharts from 'components/common/BasicEChart';
import { rgbaColor } from 'helpers/utils';

// TypeScript interfaces
interface StatsChartProps {
  color: string;
  data: number[];
}

echarts.use([
  TitleComponent,
  TooltipComponent,
  GridComponent,
  LineChart,
  CanvasRenderer
]);

const getOptions = (color: string, data: number[]) => ({
  tooltip: {
    trigger: 'axis'
  },
  series: [
    {
      type: 'line',
      data,
      symbol: 'none',
      color,
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
              color: rgbaColor(color, 0.25)
            },
            {
              offset: 1,
              color: rgbaColor(color, 0)
            }
          ]
        }
      }
    }
  ],
  xAxis: {
    type: 'category',
    data: [
      'June 01',
      'June 02',
      'June 03',
      'June 04',
      'June 05',
      'June 06',
      'June 07'
    ]
  },
  grid: { right: 0, left: 0, bottom: 0, top: 0 }
});

const StatsChart: React.FC<StatsChartProps> = ({ color, data }) => {
  return (
    <BasicECharts
      echarts={echarts}
      options={getOptions(color, data)}
      style={{ height: '2rem' }}
    />
  );
};

export default StatsChart;
