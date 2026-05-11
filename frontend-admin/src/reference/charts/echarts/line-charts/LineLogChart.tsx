
import FalconComponentCard from 'components/common/FalconComponentCard';
import { months } from 'data/common';

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
import { useAppContext } from 'providers/AppProvider';
import ReactEchart from 'components/common/ReactEchart';

echarts.use([
  TitleComponent,
  TooltipComponent,
  GridComponent,
  LineChart,
  CanvasRenderer,
  LegendComponent
]);

const chartCode = `function ChartOptions() {
  const { getThemeColor } = useAppContext();
  const data = [1272, 1301, 1402, 1216, 1086, 1236, 1219, 1330, 1367, 1416, 1297, 1204];

  const getOption = () => ({
    tooltip: {
      trigger: 'axis',
      padding: [7, 10],
      backgroundColor: getThemeColor('gray-100'),
      borderColor: getThemeColor('gray-300'),
      borderWidth: 1,
      transitionDuration: 0,
      axisPointer: {
        type: 'none'
      },
      formatter: tooltipFormatter
    },
    xAxis: {
      type: 'category',
      axisLine: {
        lineStyle: {
          color: getThemeColor('gray-300')
        }
      },
      axisLabel: {
        color: getThemeColor('gray-600')
      },
      splitLine: { show: false },
      data: Array.from(Array(10).keys()).map(item => item + 1)
    },
    yAxis: {
      type: 'log',
      axisLabel: {
        color: getThemeColor('gray-600')
      },
      splitLine: {
        lineStyle: {
          color: getThemeColor('gray-200')
        }
      }
    },
    series: [
      {
        name: 'Index Of 3',
        type: 'line',
        data: [1, 3, 9, 27, 81, 247, 741, 2223, 6669],
        symbolSize: 7,
        itemStyle: {
          color: getThemeColor('white'),
          borderColor: getThemeColor('danger'),
          borderWidth: 2
        },
        lineStyle: {
          color: getThemeColor('danger')
        },
        symbol: 'circle'
      },
      {
        name: 'Index of 2',
        type: 'line',
        data: [1, 2, 4, 8, 16, 32, 64, 128, 256],
        symbolSize: 7,
        itemStyle: {
          color: getThemeColor('white'),
          borderColor: getThemeColor('success'),
          borderWidth: 2
        },
        lineStyle: {
          color: getThemeColor('success')
        },
        symbol: 'circle'
      },
      {
        name: 'Index of 1/2',
        type: 'line',
        data: [1 / 2, 1 / 4, 1 / 8, 1 / 16, 1 / 32, 1 / 64, 1 / 128, 1 / 256, 1 / 512],
        symbolSize: 7,
        itemStyle: {
          color: getThemeColor('white'),
          borderColor: getThemeColor('info'),
          borderWidth: 2
        },
        lineStyle: {
          color: getThemeColor('info')
        },
        symbol: 'circle'
      }
    ],
    grid: {
      right: 10,
      left: 5,
      bottom: 5,
      top: 10,
      containLabel: true
    }
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

const LineLogChart = () => {
  return (
    <FalconComponentCard>
      <FalconComponentCard.Header title="Line log chart" light={false} />
      <FalconComponentCard.Body
        code={chartCode}
        language="jsx"
        scope={{
          ReactEchart,
          echarts,
          useAppContext,
          months,
          tooltipFormatter
        }}
      />
    </FalconComponentCard>
  );
};

export default LineLogChart;
