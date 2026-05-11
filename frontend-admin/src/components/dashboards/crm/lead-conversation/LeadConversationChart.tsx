
import { BarChart } from 'echarts/charts';
import {
  GridComponent,
  LegendComponent,
  TitleComponent,
  TooltipComponent
} from 'echarts/components';
import * as echarts from 'echarts/core';
import { CanvasRenderer } from 'echarts/renderers';
import { getPosition, tooltipFormatter, TooltipParam } from 'helpers/echart-utils';
import { rgbaColor } from 'helpers/utils';
import { useAppContext } from 'providers/AppProvider';
import ReactEchart from 'components/common/ReactEchart';

echarts.use([
  TitleComponent,
  TooltipComponent,
  GridComponent,
  BarChart,
  CanvasRenderer,
  LegendComponent
]);

type ThemeColorGetter = (color: string) => string;

const getOptions = (getThemeColor: ThemeColorGetter) => ({
  color: [
    rgbaColor(getThemeColor('primary'), 0.7),
    rgbaColor(getThemeColor('info'), 0.6),
    rgbaColor(getThemeColor('secondary'), 0.2),
    rgbaColor(getThemeColor('warning'), 0.6)
  ],
  legend: {
    data: ['Campaigns', 'Lead', 'Opportunity', 'Deal'],
    left: '0%',
    icon: 'circle',
    inactiveColor: getThemeColor('gray-400'),
    textStyle: { color: getThemeColor('gray-700') },
    itemGap: 10
  },
  yAxis: {
    type: 'category',
    data: [
      'kerry Ingram',
      'Bradie Pitter',
      'Harrington',
      'Ashley Shaw',
      'Jenny Horas',
      'Chris Pratt'
    ],
    axisLine: {
      show: false
    },
    boundaryGap: false,
    splitLine: {
      lineStyle: {
        color: getThemeColor('gray-200')
      }
    },
    axisTick: {
      show: false
    },
    axisLabel: {
      color: getThemeColor('gray-600')
    }
  },
  xAxis: {
    type: 'value',
    splitLine: {
      lineStyle: {
        color: getThemeColor('gray-200')
      }
    },
    axisLine: {
      show: false
    },
    axisTick: {
      show: false
    },
    axisLabel: {
      show: false
    }
  },
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
    transitionDuration: 0,
    position(pos: [number, number], params: TooltipParam | TooltipParam[], dom: HTMLElement, rect: { x: number; y: number; width: number; height: number }, size: { contentSize: [number, number]; viewSize: [number, number] }) {
      return getPosition(pos, params, dom, rect, size);
    },
    formatter: tooltipFormatter
  },

  series: [
    {
      name: 'Campaigns',
      type: 'bar',
      stack: 'total',
      data: [1405, 1300, 1620, 1430, 1500, 1520],
      barWidth: '20%'
    },
    {
      name: 'Lead',
      type: 'bar',
      stack: 'total',
      data: [320, 302, 301, 334, 340, 390],
      barWidth: '20%'
    },
    {
      name: 'Opportunity',
      type: 'bar',
      stack: 'total',
      data: [220, 182, 351, 234, 290, 300],
      barWidth: '20%'
    },
    {
      name: 'Deal',
      type: 'bar',
      stack: 'total',
      data: [120, 182, 191, 134, 190, 170],
      barWidth: '20%'
    }
  ],
  grid: {
    right: 5,
    left: 5,
    bottom: 8,
    top: 60,
    containLabel: true
  }
});

const LeadConversationChart = () => {
  const { getThemeColor } = useAppContext();
  return (
    <ReactEchart
      echarts={echarts}
      option={getOptions(getThemeColor)}
      style={{ height: '19.1rem' }}
    />
  );
};

export default LeadConversationChart;
