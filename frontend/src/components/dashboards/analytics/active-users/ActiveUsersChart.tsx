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


// TypeScript interfaces for chart data
interface ActiveUsersChartData {
  mobile: number[];
  desktop: number[];
  tablet: number[];
}

interface ActiveUsersChartProps {
  data: ActiveUsersChartData;
}

echarts.use([
  TitleComponent,
  TooltipComponent,
  GridComponent,
  LineChart,
  CanvasRenderer,
  LegendComponent
]);

const getOptions = (getThemeColor: (color: string) => string, data: ActiveUsersChartData) => ({
  color: [
    getThemeColor('primary'),
    getThemeColor('success'),
    getThemeColor('info')
  ],
  tooltip: {
    trigger: 'axis',
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
    boundaryGap: false,
    silent: true,
    axisPointer: {
      lineStyle: {
        color: getThemeColor('gray-300')
      }
    },
    splitLine: { show: false },
    axisLine: {
      lineStyle: {
        color: getThemeColor('gray-300')
      }
    },
    axisTick: {
      show: true,
      length: 20,
      lineStyle: {
        color: getThemeColor('gray-200')
      },

      interval: 5
    },
    axisLabel: {
      color: getThemeColor('gray-600'),
      formatter: (value: string) => dayjs(value).format('MMM DD'),
      align: 'left',
      fontSize: 11,
      padding: [0, 0, 0, 5],
      interval: 5
    }
  },
  yAxis: {
    type: 'value',
    position: 'right',
    axisPointer: { show: false },
    splitLine: {
      lineStyle: {
        color: getThemeColor('gray-200')
      }
    },
    axisLabel: {
      show: true,
      color: getThemeColor('gray-600'),
      formatter: (value: number) => `${Math.round((value / 1000) * 10) / 10}k`
    },
    axisTick: { show: false },
    axisLine: { show: false }
  },
  series: [
    {
      type: 'line',
      name: 'Mobile',
      data: data.mobile,
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
    },
    {
      type: 'line',
      name: 'Desktop',
      data: data.desktop,
      showSymbol: false,
      symbol: 'circle',
      itemStyle: {
        borderColor: getThemeColor('success'),
        borderWidth: 2
      },
      lineStyle: {
        color: getThemeColor('success')
      },
      symbolSize: 2
    },
    {
      type: 'line',
      name: 'Tablet',
      data: data.tablet,
      showSymbol: false,
      symbol: 'circle',
      itemStyle: {
        borderColor: getThemeColor('info'),
        borderWidth: 2
      },
      lineStyle: {
        color: getThemeColor('info')
      },
      symbolSize: 2
    }
  ],
  grid: {
    containLabel: true,
    right: '5px',
    left: 0,
    bottom: 0,
    top: 10
  }
});

const ActiveUsersChart: React.FC<ActiveUsersChartProps> = ({ data }) => {
  const { getThemeColor } = useAppContext();
  return (
    <ReactEchart
      echarts={echarts}
      option={getOptions(getThemeColor, data)}
      style={{ height: '16.25rem' }}
    />
  );
};

export default ActiveUsersChart;
