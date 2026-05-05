
import FalconComponentCard from 'components/common/FalconComponentCard';

import { BarChart } from 'echarts/charts';
import {
  GridComponent,
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
  CanvasRenderer
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
      padding: [7, 10],
      backgroundColor: getThemeColor('gray-100'),
      borderColor: getThemeColor('gray-300'),
      textStyle: { color: getThemeColor('gray-1100') },
      borderWidth: 1,
      formatter: tooltipFormatter,
      transitionDuration: 0,
      axisPointer: {
        type: 'none'
      }
    },
    xAxis: {
      type: 'value',
      boundaryGap: '0%',
      axisLine: {
        show: true,
        lineStyle: {
          color: getThemeColor('gray-300')
        }
      },
      axisTick: { show: true },
      axisLabel: {
        color: getThemeColor('gray-500')
      },
      splitLine: {
        show: false
      },
      min: 600
    },
    yAxis: {
      type: 'category',
      data: months,
      boundaryGap: '0%',
      axisLabel: {
        formatter: value => value.substring(0, 3),
        show: true,
        color: getThemeColor('gray-500'),
        margin: 15
      },
      splitLine: {
        show: true,
        lineStyle: {
          color: getThemeColor('gray-200')
        }
      },
      axisTick: { show: false },
      axisLine: {
        lineStyle: {
          color: getThemeColor('gray-300')
        }
      }
    },
    series: [
      {
        type: 'bar',
        name: 'Total',
        data,
        lineStyle: { color: getThemeColor('primary') },
        itemStyle: {
          color: getThemeColor('primary'),
          borderRadius: [0, 3, 3, 0]
        },
        showSymbol: false,
        symbol: 'circle',
        smooth: false,
        emphasis: {
          scale: true
        }
      }
    ],
    grid: { right: '3%', left: '10%', bottom: '10%', top: '5%' }
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

const HorizontalBarChart = () => {
  return (
    <FalconComponentCard className="h-100">
      <FalconComponentCard.Header title="Horizontal bar chart" light={false} />
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

export default HorizontalBarChart;
