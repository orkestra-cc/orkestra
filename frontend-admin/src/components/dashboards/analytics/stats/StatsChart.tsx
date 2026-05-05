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

echarts.use([
  TitleComponent,
  TooltipComponent,
  GridComponent,
  LineChart,
  CanvasRenderer
]);

type ThemeColorGetter = (color: string) => string;
type GridConfig = Record<string, unknown>;

const getOptions = (getThemeColor: ThemeColorGetter, data: number[], grid: GridConfig) => ({
  tooltip: {
    show: false
  },
  series: [
    {
      type: 'bar',
      data,
      symbol: 'none',
      itemStyle: {
        color: getThemeColor('primary'),
        borderRadius: [5, 5, 0, 0]
      }
    }
  ],
  grid
});

interface StatsChartProps {
  data: number[];
  grid: GridConfig;
}

const StatsChart = ({ data, grid }: StatsChartProps) => {
  const { getThemeColor } = useAppContext();
  return (
    <BasicECharts
      echarts={echarts}
      options={getOptions(getThemeColor, data, grid)}
      style={{ height: '1.875rem' }}
    />
  );
};

export default StatsChart;
