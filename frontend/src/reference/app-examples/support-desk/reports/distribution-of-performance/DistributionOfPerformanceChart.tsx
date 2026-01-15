
import { getPastDates, rgbaColor } from 'helpers/utils';
import { BarChart } from 'echarts/charts';
import {
  GridComponent,
  LegendComponent,
  TitleComponent,
  TooltipComponent
} from 'echarts/components';
import * as echarts from 'echarts/core';
import { CanvasRenderer } from 'echarts/renderers';
import dayjs from 'dayjs';
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

const getOptions = (getThemeColor: (color: string) => string, isDark: boolean) => ({
  color: [getThemeColor('primary'), isDark ? '#236EA1' : '#7DD7FE'],
  legend: {
    data: ['Agent Support', 'Group Support'],
    icon: 'circle',
    itemWidth: 10,
    itemHeight: 10,
    padding: [0, 0, 0, 0],
    textStyle: {
      color: getThemeColor('gray-700'),
      fontWeight: '500',
      fontSize: '13px'
    },
    left: 0,
    itemGap: 16
  },
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
  xAxis: {
    data: getPastDates(12).map(date => dayjs(date).format('MMM DD')),
    splitLine: { show: false },
    splitArea: { show: false },

    axisLabel: {
      color: getThemeColor('gray-600')
    },

    axisLine: {
      lineStyle: {
        color: getThemeColor('gray-300')
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
      name: 'Agent Support',
      type: 'bar',
      stack: 'one',
      emphasis: {
        itemStyle: {
          shadowColor: rgbaColor(getThemeColor('gray-1100'), 0.3)
        }
      },
      data: [50, 25, 35, 30, 45, 35, 38, 30, 35, 30, 35, 38]
    },
    {
      name: 'Group Support',
      type: 'bar',
      stack: 'one',
      emphasis: {
        itemStyle: {
          shadowColor: rgbaColor(getThemeColor('gray-1100'), 0.3)
        }
      },
      data: [45, 50, 40, 35, 50, 40, 44, 35, 40, 45, 40, 44],
      itemStyle: {
        borderRadius: [3, 3, 0, 0]
      }
    }
  ],
  barWidth: '15px',
  grid: {
    top: '15%',
    bottom: 0,
    left: 0,
    right: 0,
    containLabel: true
  }
});

const DistributionOfPerformanceChart = () => {
  const { config, getThemeColor } = useAppContext();
  const { isDark } = config;
  return (
    <ReactEchart
      echarts={echarts}
      option={getOptions(getThemeColor, isDark)}
      style={{ height: '21.875rem' }}
    />
  );
};

export default DistributionOfPerformanceChart;
