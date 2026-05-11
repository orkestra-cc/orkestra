
import FalconComponentCard from 'components/common/FalconComponentCard';

import { BarChart } from 'echarts/charts';
import {
  GridComponent,
  LegendComponent,
  TitleComponent,
  TooltipComponent
} from 'echarts/components';
import * as echarts from 'echarts/core';
import { CanvasRenderer } from 'echarts/renderers';
import { tooltipFormatter } from 'helpers/echart-utils';
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

const chartCode = `function ChartOptions() {
  const { getThemeColor } = useAppContext();
  const days = ['Monday', 'Tuesday', 'Wednesday', 'Thursday', 'Friday', 'Saturday', 'Sunday'];

  const getOption = () => ({
    color: [
      getThemeColor('info'),
      getThemeColor('danger'),
      getThemeColor('warning'),
      getThemeColor('success'),
      getThemeColor('primary')
    ],
    tooltip: {
      trigger: 'axis',
      axisPointer: {
        type: 'shadow'
      },
      padding: [7, 10],
      backgroundColor: getThemeColor('gray-100'),
      borderColor: getThemeColor('gray-300'),
      textStyle: { color: getThemeColor('primary') },
      borderWidth: 1,
      transitionDuration: 0,
      formatter: tooltipFormatter
    },
    toolbox: {
      feature: {
        magicType: {
          type: ['stack', 'tiled']
        }
      },
      right: 0
    },
    legend: {
      data: ['Direct', 'Mail Ad', 'Affiliate Ad', 'Video Ad', 'Search Engine'],
      textStyle: {
        color: getThemeColor('gray-600')
      },
      left: 0
    },
    xAxis: {
      type: 'value',
      axisLine: {
        show: true,
        lineStyle: {
          color: getThemeColor('gray-300')
        }
      },
      axisTick: { show: false },
      axisLabel: {
        color: getThemeColor('gray-500')
      },
      splitLine: {
        lineStyle: {
          show: true,
          color: getThemeColor('gray-200')
        }
      }
    },
    yAxis: {
      type: 'category',
      data: days,
      axisLine: {
        lineStyle: {
          show: true,
          color: getThemeColor('gray-300')
        }
      },
      axisTick: { show: false },
      axisLabel: {
        color: getThemeColor('gray-500'),
        formatter: value => value.substring(0, 3)
      }
    },
    series: [
      {
        name: 'Direct',
        type: 'bar',
        stack: 'total',
        label: {
          show: true,
          color: '#fff'
        },
        emphasis: {
          focus: 'series'
        },
        data: [320, 302, 301, 334, 390, 330, 320]
      },
      {
        name: 'Mail Ad',
        type: 'bar',
        stack: 'total',
        label: {
          show: true
        },
        emphasis: {
          focus: 'series'
        },
        data: [220, 188, 301, 250, 190, 230, 210]
      },
      {
        name: 'Affiliate Ad',
        type: 'bar',
        stack: 'total',
        label: {
          show: true,
          color: '#fff'
        },
        emphasis: {
          focus: 'series'
        },
        data: [220, 182, 191, 234, 290, 330, 310]
      },
      {
        name: 'Video Ad',
        type: 'bar',
        stack: 'total',
        label: {
          show: true,
          color: '#fff'
        },
        emphasis: {
          focus: 'series'
        },
        data: [150, 212, 201, 154, 190, 330, 410]
      },
      {
        name: 'Search Engine',
        type: 'bar',
        stack: 'total',
        label: {
          show: true,
        },
        emphasis: {
          focus: 'series'
        },
        data: [820, 832, 901, 934, 1290, 1330, 1320]
      }
    ],
    grid: {
      right: 15,
      left: 5,
      bottom: 5,
      top: '15%',
      containLabel: true
    }
    });
    return (
      <ReactEchart
        echarts={echarts}
        option={getOption()}
        style={{ height: '21.88rem' }}
      />
    );
  }
`;

const StackedHorizontalChart = () => {
  return (
    <FalconComponentCard className="h-100">
      <FalconComponentCard.Header
        title="Stacked horizontal bar chart"
        light={false}
      />
      <FalconComponentCard.Body
        code={chartCode}
        language="jsx"
        scope={{
          ReactEchart,
          echarts,
          useAppContext,
          tooltipFormatter
        }}
      />
    </FalconComponentCard>
  );
};

export default StackedHorizontalChart;
