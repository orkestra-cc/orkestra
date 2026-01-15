import { BarChart } from 'echarts/charts';
import {
  GridComponent,
  LegendComponent,
  TitleComponent,
  TooltipComponent
} from 'echarts/components';
import * as echarts from 'echarts/core';
import { CanvasRenderer } from 'echarts/renderers';
import { useAppContext } from 'providers/AppProvider';
import ReactEchart from 'components/common/ReactEchart';


// TypeScript interfaces
interface UsersByCountryChartProps {
  data: [string[], number[]]; // [countries, percentages]
}

echarts.use([
  TitleComponent,
  TooltipComponent,
  GridComponent,
  BarChart,
  CanvasRenderer,
  LegendComponent
]);

const getOptions = (getThemeColor: (color: string) => string, data: [string[], number[]]) => ({
  tooltip: {
    trigger: 'axis',
    padding: [7, 10],
    axisPointer: {
      type: 'none'
    },
    backgroundColor: getThemeColor('gray-100'),
    borderColor: getThemeColor('gray-300'),
    textStyle: { color: getThemeColor('gray-1100') },
    borderWidth: 1,
    transitionDuration: 0
  },
  xAxis: {
    type: 'category',
    data: data[0],
    axisLabel: {
      color: getThemeColor('gray-600'),
      formatter: (value: string) => value.substring(0, 3)
    },
    axisLine: {
      lineStyle: {
        color: getThemeColor('gray-400')
      }
    },
    axisTick: {
      show: true,
      alignWithLabel: true,
      lineStyle: {
        color: getThemeColor('gray-200')
      }
    }
  },
  yAxis: {
    type: 'value',
    axisTick: {
      show: false
    },
    splitLine: {
      lineStyle: {
        color: getThemeColor('gray-300'),
        type: 'dashed'
      }
    },
    axisLabel: {
      color: getThemeColor('gray-600'),
      formatter: (value: number) => `${value}%`,
      fontWeight: 500,
      padding: [3, 0, 0, 0],
      margin: 12
    },
    axisLine: {
      show: false
    }
  },
  series: [
    {
      type: 'bar',
      data: data[1],
      itemStyle: {
        borderRadius: [3, 3, 0, 0],
        color: getThemeColor('primary')
      },
      barWidth: 15
    }
  ],
  grid: {
    containLabel: true,
    right: 0,
    left: 0,
    bottom: 0,
    top: 15
  }
});

const UsersByCountryChart = ({ data }: UsersByCountryChartProps) => {
  const { getThemeColor } = useAppContext();
  return (
    <ReactEchart
      echarts={echarts}
      option={getOptions(getThemeColor, data)}
      style={{ height: '13.125rem' }}
    />
  );
};

export default UsersByCountryChart;
