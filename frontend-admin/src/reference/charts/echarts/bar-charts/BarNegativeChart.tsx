
import FalconComponentCard from 'components/common/FalconComponentCard';

import { BarChart } from 'echarts/charts';

import * as echarts from 'echarts/core';

import {
  GridComponent,
  LegendComponent,
  TitleComponent,
  TooltipComponent
} from 'echarts/components';
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
  const months = [
    'January',
    'February',
    'March',
    'April',
    'May',
    'June',
    'July',
    'August',
    'September',
    'October',
    'November',
    'December'
  ];

  const data = [1272, 1301, 1402, 1216, 1086, 1236, 1219, 1330, 1367, 1416, 1297, 1204];

  const getOption = () => ({
    tooltip: {
      trigger: 'axis',
      axisPointer: {
        type: 'shadow'
      },
      padding: [7, 10],
      backgroundColor: getThemeColor('gray-100'),
      borderColor: getThemeColor('gray-300'),
      textStyle: { color: getThemeColor('gray-1100') },
      borderWidth: 1,
      transitionDuration: 0,
      formatter: tooltipFormatter
    },
    grid: {
      top: 5,
      bottom: 5,
      left: 5,
      right: 5
    },
    xAxis: {
      type: 'value',
      position: 'top',
      splitLine: {
        lineStyle: {
          type: 'dashed',
          color: getThemeColor('gray-200')
        }
      }
    },
    yAxis: {
      type: 'category',
      axisLine: { show: false },
      axisLabel: { show: false },
      axisTick: { show: false },
      splitLine: { show: false },
      data: ['Ten', 'Nine', 'Eight', 'Seven', 'Six', 'Five', 'Four', 'Three', 'Two', 'One']
    },
    series: [
      {
        name: 'Cost',
        type: 'bar',
        stack: 'total',
        label: {
          show: true,
          formatter: '{b}',
          color: '#fff'
        },
        itemStyle: {
          color: getThemeColor('primary')
        },
        data: [-0.12, -0.19, 0.2, 0.44, -0.23, 0.08, -0.17, 0.47, -0.36, 0.18]
      }
    ]
  });
    
    return (
      <ReactEchart
        echarts={echarts}
        option={getOption()}
        style={{ height: '18.75rem' }}
      />
    );
  }
`;

const BarNegativeChart = () => {
  return (
    <FalconComponentCard className="h-100">
      <FalconComponentCard.Header
        title="Bar chart with negative value"
        light={false}
      />
      <FalconComponentCard.Body
        code={chartCode}
        language="jsx"
        scope={{
          ReactEchart,
          echarts,
          tooltipFormatter,
          useAppContext
        }}
      />
    </FalconComponentCard>
  );
};

export default BarNegativeChart;
