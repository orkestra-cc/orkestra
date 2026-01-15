
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
  const getOption = () => ({
    color: [getThemeColor('primary'), getThemeColor('info')],
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
      xAxis: {
        type: 'value',
        axisLabel: {
          formatter: value => value / 1000+'k',
          color: getThemeColor('gray-500')
        },
        axisLine: {
          show: true,
          lineStyle: {
            color: getThemeColor('gray-300'),
            type: 'solid'
          }
        },
        splitLine: {
          lineStyle: {
            type: 'dashed',
            color: getThemeColor('gray-200')
          }
        }
      },
      yAxis: {
        type: 'category',
        axisLine: {
          show: true,
          lineStyle: {
            color: getThemeColor('gray-300'),
            type: 'solid'
          }
        },
        axisLabel: {
          color: getThemeColor('gray-500')
        },
        axisTick: { show: false },
        splitLine: { show: false },
        data: ['Brazil', 'Indonesia', 'USA', 'India', 'China']
      },
      series: [
        {
          name: '2011',
          type: 'bar',
          data: [18203, 23489, 29034, 104970, 131744],
          itemStyle: {
            borderRadius: [0, 3, 3, 0]
          }
        },
        {
          name: '2012',
          type: 'bar',
          data: [19325, 23438, 31000, 121594, 134141],
          itemStyle: {
            borderRadius: [0, 3, 3, 0]
          }
        }
      ],
      grid: { right: 15, left: '12%', bottom: '10%', top: 5 }
    });

    return (
      <ReactEchart
        echarts={echarts}
        option={getOption()}
        style={{ minHeight: '18.75rem' }}
      />
    );
  }
`;

const SeriesBarChart = () => {
  return (
    <FalconComponentCard className="h-100">
      <FalconComponentCard.Header title="Series bar chart" light={false} />
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

export default SeriesBarChart;
