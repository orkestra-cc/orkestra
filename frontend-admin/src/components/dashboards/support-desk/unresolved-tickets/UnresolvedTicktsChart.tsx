
import { BarChart } from 'echarts/charts';
import {
  GridComponent,
  LegendComponent,
  TitleComponent,
  TooltipComponent
} from 'echarts/components';
import { CanvasRenderer } from 'echarts/renderers';
import * as echarts from 'echarts/core';
import { rgbaColor } from 'helpers/utils';
import { useAppContext } from 'providers/AppProvider';
import ReactEchart from 'components/common/ReactEchart';
import { forwardRef } from 'react';
import ReactEChartsCore from 'echarts-for-react/lib/core';

echarts.use([
  TitleComponent,
  TooltipComponent,
  GridComponent,
  BarChart,
  CanvasRenderer,
  LegendComponent
]);

type ThemeColorGetter = (color: string) => string;

interface UnresolvedTicktsChartProps {
  data: number[][];
}

const getOption = (getThemeColor: ThemeColorGetter, data: number[][], isDark: boolean) => ({
  color: [
    getThemeColor('primary'),
    getThemeColor('info'),
    isDark ? '#229BD2' : '#73D3FE',
    isDark ? '#195979' : '#A9E4FF'
  ],
  tooltip: {
    trigger: 'item',
    padding: [7, 10],
    backgroundColor: getThemeColor('gray-100'),
    borderColor: getThemeColor('gray-300'),
    textStyle: { color: getThemeColor('gray-900') },
    borderWidth: 1,
    transitionDuration: 0,
    axisPointer: {
      type: 'none'
    }
  },
  legend: {
    show: false
  },
  xAxis: {
    data: ['Mon', 'Tue', 'Wed', 'Thu', 'Fri', 'Sat', 'Sun'],
    splitLine: { show: false },
    splitArea: { show: false },

    axisLabel: {
      color: getThemeColor('gray-600'),
      margin: 8
    },

    axisLine: {
      lineStyle: {
        color: getThemeColor('gray-300'),
        type: 'dashed'
      }
    },
    axisTick: {
      show: false
    }
  },
  yAxis: {
    splitLine: {
      lineStyle: {
        color: getThemeColor('gray-300'),
        type: 'dashed'
      }
    },
    axisLabel: {
      color: getThemeColor('gray-600')
    },
    position: 'right'
  },
  series: [
    {
      name: 'Urgent',
      type: 'bar',
      stack: 'one',
      emphasis: {
        itemStyle: {
          shadowColor: rgbaColor(getThemeColor('gray-1100'), 0.3)
        }
      },
      data: data[0]
    },
    {
      name: 'High',
      type: 'bar',
      stack: 'one',
      emphasis: {
        itemStyle: {
          shadowColor: rgbaColor(getThemeColor('gray-1100'), 0.3)
        }
      },
      data: data[1]
    },
    {
      name: 'Medium',
      type: 'bar',
      stack: 'one',
      emphasis: {
        itemStyle: {
          shadowColor: rgbaColor(getThemeColor('gray-1100'), 0.3)
        }
      },
      data: data[2]
    },
    {
      name: 'Low',
      type: 'bar',
      stack: 'one',
      emphasis: {
        itemStyle: {
          shadowColor: rgbaColor(getThemeColor('gray-1100'), 0.3)
        }
      },
      data: data[3],
      itemStyle: {
        borderRadius: [2, 2, 0, 0]
      }
    }
  ],

  barWidth: '15px',
  grid: {
    top: '8%',
    bottom: 18,
    left: 0,
    right: 2,
    containLabel: true
  }
});

const UnresolvedTicktsChart = forwardRef<ReactEChartsCore, UnresolvedTicktsChartProps>(
  ({ data }, ref) => {
    const { config, getThemeColor } = useAppContext();

    const { isDark } = config;
    return (
      <ReactEchart
        echarts={echarts}
        ref={ref}
        option={getOption(getThemeColor, data, isDark)}
        style={{ height: '21rem' }}
      />
    );
  }
);

UnresolvedTicktsChart.displayName = 'UnresolvedTicktsChart';

export default UnresolvedTicktsChart;
