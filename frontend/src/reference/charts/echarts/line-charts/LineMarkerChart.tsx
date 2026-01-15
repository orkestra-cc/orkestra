
import FalconComponentCard from 'components/common/FalconComponentCard';
import { LineChart } from 'echarts/charts';
import {
  GridComponent,
  LegendComponent,
  MarkLineComponent,
  MarkPointComponent,
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
  LegendComponent,
  MarkPointComponent,
  MarkLineComponent
]);

const chartCode = `function ChartOptions() {
  const { getThemeColor } = useAppContext();
  const days = ['Monday', 'Tuesday', 'Wednesday', 'Thursday', 'Friday', 'Saturday', 'Sunday'];
  
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
  
  const getOption = () => ({
    color: [
      getThemeColor('primary'),
      getThemeColor('warning')
    ],
    legend: {
      data: [
        {
          name: 'Max',
          textStyle: {
            color: getThemeColor('gray-600')
          }
        },
        {
          name: 'Min',
          textStyle: {
            color: getThemeColor('gray-600')
          }
        }
      ]
    },
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
      data: days,
      boundaryGap: false,
      axisLine: {
        lineStyle: {
          color: getThemeColor('gray-300'),
          type: 'solid'
        }
      },
      axisTick: { show: false },
      axisLabel: {
        formatter: value => value.substring(0, 3),
        color: getThemeColor('gray-400'),
        margin: 15
      },
      splitLine: {
        show: false
      }
    },
    yAxis: {
      type: 'value',
      splitLine: {
        lineStyle: {
          color: getThemeColor('gray-200'),
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
    series: [
      {
        name: 'Max',
        type: 'line',
        data: [10, 11, 13, 11, 12, 9, 12],
        symbolSize: 10,
        itemStyle: {
          color: getThemeColor('white'),
          borderColor: getThemeColor('primary'),
          borderWidth: 2
        },
        lineStyle: {
          color: getThemeColor('primary')
        },
        symbol: 'circle',
        markPoint: {
          itemStyle: {
            color: getThemeColor('primary')
          },
          data: [
            { type: 'max', name: 'Max' },
            { type: 'min', name: 'Min' }
          ]
        },
        markLine: {
          lineStyle: {
            color: getThemeColor('primary')
          },
          label: {
            color: getThemeColor('gray-600')
          },
          data: [{ type: 'average', name: 'average' }]
        }
      },
      {
        name: 'Min',
        type: 'line',
        data: [1, -2, 2, 5, 3, 2, 0],
        symbolSize: 10,
        itemStyle: {
          color: getThemeColor('white'),
          borderColor: getThemeColor('danger'),
          borderWidth: 2
        },
        lineStyle: {
          color: getThemeColor('danger')
        },
        symbol: 'circle',
        markPoint: {
          itemStyle: {
            color: getThemeColor('danger')
          },
          label: {
            color: '#fff'
          },
          data: [{ name: 'Weekly lowest', value: -2, xAxis: 1, yAxis: -1.5 }]
        },
        markLine: {
          lineStyle: {
            color: getThemeColor('danger')
          },
          label: {
            color: getThemeColor('gray-600')
          },
          data: [
            { type: 'average', name: 'average' },
            [
              {
                symbol: 'none',
                x: '90%',
                yAxis: 'max'
              },
              {
                symbol: 'circle',
                label: {
                  position: 'start',
                  formatter: 'Max'
                },
                type: 'max',
                name: 'Highest point'
              }
            ]
          ]
        }
      }
    ],
    grid: { right: '8%', left: '5%', bottom: '10%', top: '15%' }
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

const LineMarkerChart = () => {
  return (
    <FalconComponentCard>
      <FalconComponentCard.Header title="Line marker chart" light={false} />
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

export default LineMarkerChart;
