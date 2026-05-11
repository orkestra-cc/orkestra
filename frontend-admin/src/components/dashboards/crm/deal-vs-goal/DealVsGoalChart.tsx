
import dayjs from 'dayjs';
import * as echarts from 'echarts/core';
import { getPosition, tooltipFormatter, TooltipParam } from 'helpers/echart-utils';
import { getPastDates, rgbaColor } from 'helpers/utils';
import { useBreakpoints } from 'hooks/ui/useBreakpoints';
import { useAppContext } from 'providers/AppProvider';
import ReactEchart from 'components/common/ReactEchart';

type ThemeColorGetter = (color: string) => string;
type DealVsGoalData = { closedAmount: number[]; revenueGoal: number[] };

const getOptions = (getThemeColor: ThemeColorGetter, data: DealVsGoalData) => ({
  color: [getThemeColor('primary'), getThemeColor('warning')],
  tooltip: {
    trigger: 'axis',
    padding: [7, 10],
    backgroundColor: getThemeColor('gray-100'),
    borderColor: getThemeColor('gray-300'),
    textStyle: { color: getThemeColor('gray-1100') },
    borderWidth: 1,
    formatter: tooltipFormatter,
    transitionDuration: 0,
    position(pos: [number, number], params: TooltipParam | TooltipParam[], dom: HTMLElement, rect: { x: number; y: number; width: number; height: number }, size: { contentSize: [number, number]; viewSize: [number, number] }) {
      return getPosition(pos, params, dom, rect, size);
    }
  },

  legend: {
    left: 'left',
    top: -5,
    data: ['Closed Amount', 'Revenue Goal'],
    itemWidth: 10,
    itemHeight: 10,
    icon: 'circle',
    inactiveColor: getThemeColor('gray-400'),
    textStyle: { color: getThemeColor('gray-700') },
    itemGap: 20
  },
  xAxis: {
    type: 'category',
    name: 'Closed Date',
    nameGap: 50,
    nameLocation: 'center',
    offset: 0,
    nameTextStyle: {
      color: getThemeColor('gray-700')
    },
    data: getPastDates(9),
    boundaryGap: false,
    axisPointer: {
      lineStyle: {
        color: getThemeColor('gray-300'),
        type: 'dashed'
      }
    },
    splitLine: { show: false },
    axisLine: {
      lineStyle: {
        color: rgbaColor('#000', 0.01),
        type: 'dashed'
      }
    },
    axisTick: { show: false },
    axisLabel: {
      color: getThemeColor('gray-400'),
      align: 'right',
      formatter: (value: string) => dayjs(value).format('DD MMM, YY'),
      margin: 20
    }
  },
  yAxis: {
    type: 'value',
    name: 'Closed Amount',
    nameGap: 85,
    nameLocation: 'middle',
    nameTextStyle: {
      color: getThemeColor('gray-700')
    },
    splitNumber: 3,
    axisPointer: { show: false },
    splitLine: {
      lineStyle: {
        color: getThemeColor('gray-200')
      }
    },
    boundaryGap: false,
    axisLabel: {
      show: true,
      color: getThemeColor('gray-400'),
      formatter: function (value: number) {
        return `$${value}`;
      },
      margin: 15
    },
    axisTick: { show: false },
    axisLine: { show: false }
  },
  series: [
    {
      type: 'line',
      name: 'Closed Amount',
      data: data.closedAmount,
      symbolSize: 5,
      symbol: 'circle',
      smooth: false,
      lineStyle: { color: rgbaColor(getThemeColor('primary')) },
      itemStyle: {
        borderColor: rgbaColor(getThemeColor('primary'), 0.6),
        borderWidth: 2
      },
      emphasis: {
        scale: true
      }
    },
    {
      type: 'line',
      name: 'Revenue Goal',
      data: data.revenueGoal,
      symbolSize: 5,
      symbol: 'circle',
      smooth: false,
      emphasis: {
        scale: true
      },
      lineStyle: { color: rgbaColor(getThemeColor('warning')) },
      itemStyle: {
        borderColor: rgbaColor(getThemeColor('warning'), 0.6),
        borderWidth: 2
      }
    }
  ],
  grid: {
    right: 5,
    left: 100,
    bottom: 30,
    top: 30
  }
});

interface DealVSGoalChartProps {
  data: DealVsGoalData;
}

const DealVSGoalChart = ({ data }: DealVSGoalChartProps) => {
  const { breakpoints } = useBreakpoints();
  const { getThemeColor } = useAppContext();
  return (
    <ReactEchart
      echarts={echarts}
      option={getOptions(getThemeColor, data)}
      style={{ height: breakpoints.up('xxl') ? '14rem' : '15rem' }}
    />
  );
};

export default DealVSGoalChart;
