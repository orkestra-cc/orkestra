
import { BarChart } from 'echarts/charts';
import {
  GridComponent,
  LegendComponent,
  TitleComponent,
  TooltipComponent
} from 'echarts/components';
import { CanvasRenderer } from 'echarts/renderers';
import * as echarts from 'echarts/core';
import { getPastDates, rgbaColor } from 'helpers/utils';
import dayjs from 'dayjs';
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

interface TooltipParams {
  seriesName: string;
  borderColor?: string;
  color: string;
  name: string;
  value: number;
}

interface NumberOfTicketsChartProps {
  data: number[][];
}

const getOption = (getThemeColor: ThemeColorGetter, data: number[][], isDark: boolean) => ({
  color: [
    getThemeColor('primary'),
    isDark ? '#1E4C88' : '#94BCF1',
    isDark ? '#1A3A64' : '#C0D8F7',
    isDark ? '#225FAE' : '#6AA3ED'
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
    },
    formatter: function (params: TooltipParams) {
      return `<div>${params.seriesName}</div>
      <div class="fs-10 d-flex flex-between-center">
      <div>
      <div class="dot me-1 fs-11 d-inline-block" style="background-color:${
        params.borderColor ? params.borderColor : params.color
      }"></div>
        <span>${dayjs(params.name).format('MMM DD')} : </span>
        </div>
      <strong>${params.value}</strong>
      </div>`;
    }
  },
  legend: {
    show: false
  },
  xAxis: {
    data: getPastDates(6),
    splitLine: { show: false },
    splitArea: { show: false },
    axisLabel: {
      formatter: (value: string) => dayjs(value).format('MMM DD'),
      color: getThemeColor('gray-600')
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
    }
  },
  series: [
    {
      name: 'On Hold Tickets',
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
      name: 'Open Tickets',
      type: 'bar',
      stack: 'two',
      emphasis: {
        itemStyle: {
          shadowColor: rgbaColor(getThemeColor('gray-1100'), 0.3)
        }
      },
      data: data[1]
    },
    {
      name: 'Due Tickets',
      type: 'bar',
      stack: 'three',
      emphasis: {
        itemStyle: {
          shadowColor: rgbaColor(getThemeColor('gray-1100'), 0.3)
        }
      },
      data: data[2]
    },
    {
      name: 'Unassigned Tickets',
      type: 'bar',
      stack: 'four',
      emphasis: {
        itemStyle: {
          shadowColor: rgbaColor(getThemeColor('gray-1100'), 0.3)
        }
      },
      data: data[3]
    }
  ],
  itemStyle: {
    borderRadius: [3, 3, 0, 0]
  },

  barWidth: '12px',
  grid: {
    top: '10%',
    bottom: 0,
    left: 0,
    right: 0,
    containLabel: true
  }
});

const NumberOfTicketsChart = forwardRef<ReactEChartsCore, NumberOfTicketsChartProps>(
  ({ data }, ref) => {
    const { config, getThemeColor } = useAppContext();
    const { isDark } = config;
    return (
      <ReactEchart
        echarts={echarts}
        option={getOption(getThemeColor, data, isDark)}
        ref={ref}
        style={{ height: '18.5rem', minWidth: '40rem' }}
      />
    );
  }
);

NumberOfTicketsChart.displayName = 'NumberOfTicketsChart';

export default NumberOfTicketsChart;
