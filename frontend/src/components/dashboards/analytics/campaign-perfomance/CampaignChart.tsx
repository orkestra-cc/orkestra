
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

echarts.use([
  TitleComponent,
  TooltipComponent,
  GridComponent,
  LineChart,
  CanvasRenderer
]);

const getOptions = (color: string, data: number[]) => ({
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
    boundaryGap: false
  },
  grid: { right: '20px', left: '0', bottom: '0', top: '20px' }
});

interface CampaignChartProps {
  color: string;
  data: number[];
}

const CampaignChart = ({ color, data }: CampaignChartProps) => {
  return (
    <BasicECharts
      echarts={echarts}
      options={getOptions(color, data)}
      style={{ height: '3.125rem' }}
    />
  );
};

export default CampaignChart;
