import ReactEchart from 'components/common/ReactEchart';
import dayjs from 'dayjs';
import { LineChart } from 'echarts/charts';
import {
  GridComponent,
  LegendComponent,
  TitleComponent,
  TooltipComponent
} from 'echarts/components';
import * as echarts from 'echarts/core';
import { CanvasRenderer } from 'echarts/renderers';
import { tooltipFormatter } from 'helpers/echart-utils';
import { getPastDates } from 'helpers/utils';
import { useAppContext } from 'providers/AppProvider';


// TypeScript interfaces
interface BounceRateChartProps {
  data: number[];
}

echarts.use([
  TitleComponent,
  TooltipComponent,
  GridComponent,
  LineChart,
  CanvasRenderer,
  LegendComponent
]);

const getOptions = (getThemeColor: (color: string) => string, data: number[]) => ({
  color: getThemeColor('white'),
  title: {
    text: 'Bounce Rate',
    padding: [5, 0, 0, 0],
    textStyle: {
      color: getThemeColor('gray-900'),
      fontSize: 13,
      fontWeight: 600
    }
  },
  tooltip: {
    trigger: 'axis',
    axisPointer: {
      type: 'none'
    },
    padding: [7, 10],
    backgroundColor: getThemeColor('gray-100'),
    borderColor: getThemeColor('gray-300'),
    textStyle: { color: getThemeColor('gray-1100') },
    borderWidth: 1,
    transitionDuration: 0,
    formatter: tooltipFormatter
  },
  xAxis: {
    type: 'category',
    data: getPastDates(30).map(date => dayjs(date).format('DD MMM, YYYY')),
    axisPointer: {
      lineStyle: {
        color: getThemeColor('gray-300')
      }
    },
    splitLine: { show: false },
    axisLine: {
      lineStyle: {
        color: getThemeColor('gray-400')
      }
    },
    axisTick: {
      show: false
    },
    axisLabel: {
      color: getThemeColor('gray-600'),
      formatter: (value: string) => dayjs(value).format('MMM DD'),
      fontSize: 11
    }
  },
  yAxis: {
    type: 'value',
    axisPointer: { show: false },
    splitLine: {
      lineStyle: {
        color: getThemeColor('gray-200')
      }
    },
    axisLabel: {
      show: true,
      color: getThemeColor('gray-600'),
      formatter: (value: number) => `${value}%`,
      margin: 15
    },
    axisTick: { show: false },
    axisLine: { show: false }
  },
  series: [
    {
      type: 'line',
      name: 'Rate',
      data,
      showSymbol: false,
      symbol: 'circle',
      itemStyle: {
        borderColor: getThemeColor('primary'),
        borderWidth: 2
      },
      lineStyle: {
        color: getThemeColor('primary')
      },
      symbolSize: 2
    }
  ],
  grid: { right: '10px', left: '40px', bottom: '10%', top: '13%' }
});

const BounceRateChart: React.FC<BounceRateChartProps> = ({ data }) => {
  const { getThemeColor } = useAppContext();
  return (
    <ReactEchart
      echarts={echarts}
      option={getOptions(getThemeColor, data)}
      style={{ height: '20rem' }}
    />
  );
};

export default BounceRateChart;
