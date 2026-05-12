
import OrkestraComponentCard from 'components/common/OrkestraComponentCard';
import dayjs from 'dayjs';

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
import { rgbaColor } from 'helpers/utils';
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

  const getOption = () => ({
    tooltip: {
      trigger: 'axis',
      padding: [7, 10],
      backgroundColor: getThemeColor('gray-100'),
      borderColor: getThemeColor('gray-300'),
      textStyle: { color: getThemeColor('gray-1100') },
      borderWidth: 1,
      transitionDuration: 0,
      axisPointer: {
        type: 'none'
      },
      formatter: tooltipFormatter
    },
    xAxis: {
      type: 'category',
      boundaryGap: false,
      axisLine: {
        lineStyle: {
          color: getThemeColor('gray-300'),
          type: 'solid'
        }
      },
      axisTick: { show: false },
      axisLabel: {
        color: getThemeColor('gray-400'),
        margin: 15,
        formatter: value => dayjs(value).format('MMM DD')
      },
      splitLine: {
        show: false
      }
    },
    yAxis: {
      type: 'value',
      splitLine: {
        lineStyle: {
          color: getThemeColor('gray-200')
        }
      },
      boundaryGap: false,
      axisLabel: {
        show: true,
        color: getThemeColor('gray-400'),
        margin: 15
      },
      axisTick: { show: false },
      axisLine: { show: false }
    },
    visualMap: {
      type: 'piecewise',
      show: false,
      dimension: 0,
      seriesIndex: 0,
      pieces: [
        {
          gt: 1,
          lt: 3,
          color: rgbaColor(getThemeColor('primary'), 0.4)
        },
        {
          gt: 5,
          lt: 7,
          color: rgbaColor(getThemeColor('primary'), 0.4)
        }
      ]
    },
    series: [
      {
        type: 'line',
        name: 'Total',
        smooth: 0.6,
        symbol: 'none',
        lineStyle: {
          color: getThemeColor('primary'),
          width: 5
        },
        markLine: {
          symbol: ['none', 'none'],
          label: { show: false },
          data: [{ xAxis: 1 }, { xAxis: 3 }, { xAxis: 5 }, { xAxis: 7 }]
        },
        areaStyle: {},
        data: [
          ['2019-10-10', 200],
          ['2019-10-11', 560],
          ['2019-10-12', 750],
          ['2019-10-13', 580],
          ['2019-10-14', 250],
          ['2019-10-15', 300],
          ['2019-10-16', 450],
          ['2019-10-17', 300],
          ['2019-10-18', 100]
        ]
      }
    ],
    grid: { right: 20, left: 5, bottom: 5, top: 8, containLabel: true }
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

const AreaPiecesChart = () => {
  return (
    <OrkestraComponentCard>
      <OrkestraComponentCard.Header title="Area pieces chart" light={false} />
      <OrkestraComponentCard.Body
        code={chartCode}
        language="jsx"
        scope={{
          ReactEchart,
          echarts,
          useAppContext,
          dayjs,
          tooltipFormatter,
          rgbaColor
        }}
      />
    </OrkestraComponentCard>
  );
};

export default AreaPiecesChart;
