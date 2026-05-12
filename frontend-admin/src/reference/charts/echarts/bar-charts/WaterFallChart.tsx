
import OrkestraComponentCard from 'components/common/OrkestraComponentCard';
import dayjs from 'dayjs';

import { BarChart } from 'echarts/charts';
import {
  GridComponent,
  LegendComponent,
  TitleComponent,
  TooltipComponent
} from 'echarts/components';
import * as echarts from 'echarts/core';
import { CanvasRenderer } from 'echarts/renderers';
import { useAppContext } from 'providers/AppProvider';
import ReactEchart from 'components/common/ReactEchart';

echarts.use([
  BarChart,
  TitleComponent,
  TooltipComponent,
  GridComponent,
  CanvasRenderer,
  LegendComponent
]);

const chartCode = `function ChartOptions() {
  const { getThemeColor } = useAppContext();

  const days = [
    '2021-06-05',
    '2021-06-06',
    '2021-06-07',
    '2021-06-08',
    '2021-06-09',
    '2021-06-10',
    '2021-06-11',
    '2021-06-12',
    '2021-06-13',
    '2021-06-14',
    '2021-06-15'
  ];

  const getOption = () => ({
    legend: {
      data: ['Expenditure', 'Income'],
      textStyle: {
        color: getThemeColor('gray-600')
      }
    },
    tooltip: {
      trigger: 'axis',
      padding: [7, 10],
      backgroundColor: getThemeColor('gray-100'),
      borderColor: getThemeColor('gray-300'),
      textStyle: { color: getThemeColor('gray-1100') },
      borderWidth: 1,
      /* eslint-disable prefer-destructuring */
      formatter: function (params) {
        var tar;
        if (params[1].value !== '-') {
          tar = params[1];
        } else {
          tar = params[2];
        }
        return (
          dayjs(tar.name).format('MMM DD') + '<br/>' + tar.seriesName + ' : ' + tar.value
        );
      },
      transitionDuration: 0,
      axisPointer: {
        type: 'shadow'
      }
    },
    xAxis: {
      type: 'category',
      data: days,
      axisLine: {
        lineStyle: {
          color: getThemeColor('gray-300'),
          type: 'solid'
        }
      },
      axisTick: { show: false },
      axisLabel: {
        color: getThemeColor('gray-400'),
        formatter: value => dayjs(value).format('MMM DD'),
        margin: 15
      },
      splitLine: {
        show: false
      }
    },
    yAxis: {
      type: 'value',
      boundaryGap: '0%',
      axisLabel: {
        show: true,
        color: getThemeColor('gray-400'),
        margin: 15
      },
      splitLine: {
        show: true,
        lineStyle: {
          color: getThemeColor('gray-200')
        }
      },
      axisTick: { show: false },
      axisLine: { show: false },
      min: 600
    },
    series: [
      {
        name: 'Assist',
        type: 'bar',
        stack: 'Total',
        itemStyle: {
          borderColor: 'transparent',
          color: 'transparent'
        },
        emphasis: {
          itemStyle: {
            borderColor: 'transparent',
            color: 'transparent'
          }
        },
        data: [0, 900, 1245, 1530, 1376, 1376, 1511, 1689, 1856, 1495, 1292]
      },
      {
        name: 'Income',
        type: 'bar',
        stack: 'Total',
        label: {
          show: true,
          position: 'top',
          color: getThemeColor('gray-600')
        },
        data: [900, 345, 393, '-', '-', 135, 178, 286, '-', '-', '-'],
        itemStyle: {
          color: getThemeColor('primary'),
          borderRadius: [3, 3, 0, 0]
        }
      },
      {
        name: 'Expenditure',
        type: 'bar',
        stack: 'Total',
        label: {
          show: true,
          position: 'bottom',
          color: getThemeColor('gray-600')
        },
        data: ['-', '-', '-', 108, 154, '-', '-', '-', 119, 361, 203],
        itemStyle: {
          color: getThemeColor('success'),
          borderRadius: [3, 3, 0, 0]
        }
      }
    ],
    grid: { right: '3%', left: '10%', bottom: '10%', top: '10%' }
  }); 

  return (
    <ReactEchart
    echarts={echarts}
    option={getOption()}
    style={{ height: '21.88rem' }}
  />
  );
}`;

const WaterFallChart = () => {
  return (
    <OrkestraComponentCard className="h-100">
      <OrkestraComponentCard.Header title="Water fall chart" light={false} />
      <OrkestraComponentCard.Body
        code={chartCode}
        language="jsx"
        scope={{
          ReactEchart,
          echarts,
          useAppContext,
          dayjs
        }}
      />
    </OrkestraComponentCard>
  );
};

export default WaterFallChart;
