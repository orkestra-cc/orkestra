
import { useAppContext } from 'providers/AppProvider';
import { LineChart } from 'echarts/charts';
import {
  GridComponent,
  LegendComponent,
  TooltipComponent
} from 'echarts/components';
import * as echarts from 'echarts/core';
import { rgbaColor } from 'helpers/utils';
import ReactEchart from 'components/common/ReactEchart';
import { CSSProperties, forwardRef } from 'react';
import ReactEChartsCore from 'echarts-for-react/lib/core';

echarts.use([LineChart, TooltipComponent, GridComponent, LegendComponent]);

type ThemeColorGetter = (color: string) => string;

interface LinePaymentData {
  successful: number[];
  failed: number[];
  all: number[];
  [key: string]: number[];
}

interface TooltipParams {
  axisValue: string;
  value: number;
}

interface LinePaymentChartProps {
  data: LinePaymentData;
  paymentStatus: string;
  style?: CSSProperties;
}

const getOption = (getThemeColor: ThemeColorGetter, data: LinePaymentData, paymentStatus: string, isDark: boolean) => ({
  tooltip: {
    trigger: 'axis',
    axisPointer: {
      type: 'none'
    },
    padding: [7, 10],
    backgroundColor: getThemeColor('gray-100'),
    borderColor: getThemeColor('gray-300'),
    borderWidth: 1,
    transitionDuration: 0,
    textStyle: {
      fontWeight: 500,
      fontSize: 12,
      color: getThemeColor('gray-1100')
    },
    formatter: (params: TooltipParams[]) => `${params[0].axisValue} - ${params[0].value} USD`
  },
  xAxis: {
    type: 'category',
    data: [
      '9:00 AM',
      '10:00 AM',
      '11:00 AM',
      '12:00 PM',
      '1:00 PM',
      '2:00 PM',
      '3:00 PM',
      '4:00 PM',
      '5:00 PM',
      '6:00 PM',
      '7:00 PM',
      '8:00 PM'
    ],
    boundaryGap: false,
    splitLine: {
      show: true,
      lineStyle: {
        color: rgbaColor('#fff', 0.1)
      },
      interval: 0
    },
    axisLine: {
      lineStyle: {
        color: rgbaColor('#fff', 0.1)
      }
    },
    axisTick: {
      show: true,
      length: 10,
      lineStyle: {
        color: rgbaColor('#fff', 0.1)
      }
    },
    axisLabel: {
      color: getThemeColor('gray-400'),
      fontWeight: 600,
      fontSize: 12,
      margin: 15,
      // interval: window.innerWidth < 768 ? 'auto' : 0,
      formatter: (value: string) => value.substring(0, value.length - 3)
    }
  },
  yAxis: {
    type: 'value',
    axisPointer: {
      show: false
    },
    splitLine: {
      show: false
    },
    axisLabel: {
      show: false
    },
    axisTick: { show: false },
    axisLine: { show: false }
  },
  series: [
    {
      type: 'line',
      smooth: true,
      data: data[paymentStatus].map((item: number) => (item * 3.24).toFixed(2)),
      symbol: 'emptyCircle',
      itemStyle: {
        color: isDark ? getThemeColor('primary') : getThemeColor('white')
      },
      lineStyle: {
        color: isDark
          ? rgbaColor(getThemeColor('primary'), 0.8)
          : rgbaColor(getThemeColor('white'), 0.8)
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
              color: isDark
                ? rgbaColor(getThemeColor('primary'), 0.5)
                : rgbaColor('#fff', 0.5)
            },
            {
              offset: 1,
              color: isDark
                ? rgbaColor(getThemeColor('primary'), 0)
                : rgbaColor('#fff', 0)
            }
          ]
        }
      },
      emphasis: {
        lineStyle: {
          width: 2
        }
      }
    }
  ],
  grid: { right: 15, left: 15, bottom: '15%', top: 0 }
});

const LinePaymentChart = forwardRef<ReactEChartsCore, LinePaymentChartProps>((
  { data, paymentStatus, style },
  ref
) => {
  const {
    config: { isDark },
    getThemeColor
  } = useAppContext();

  return (
    <ReactEchart
      echarts={echarts}
      ref={ref}
      option={getOption(getThemeColor, data, paymentStatus, isDark)}
      style={style}
    />
  );
});

LinePaymentChart.displayName = 'LinePaymentChart';

export default LinePaymentChart;
