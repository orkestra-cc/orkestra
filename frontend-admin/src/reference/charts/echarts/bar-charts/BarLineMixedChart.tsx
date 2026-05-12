
import OrkestraComponentCard from 'components/common/OrkestraComponentCard';
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
  BarChart,
  TitleComponent,
  TooltipComponent,
  GridComponent,
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

  const getOption = () => ({
    tooltip: {
      trigger: 'axis',
      axisPointer: {
        type: 'cross',
        crossStyle: {
          color: getThemeColor('gray-500')
        },
        label: {
          show: true,
          backgroundColor: getThemeColor('gray-600'),
          color: getThemeColor('gray-100')
        }
      },
      padding: [7, 10],
      backgroundColor: getThemeColor('gray-100'),
      borderColor: getThemeColor('gray-300'),
      textStyle: { color: getThemeColor('gray-1100') },
      borderWidth: 1,
      transitionDuration: 0,
      formatter: tooltipFormatter
    },
    toolbox: {
      top: 0,
      feature: {
        dataView: { show: false },
        magicType: {
          show: true,
          type: ['line', 'bar']
        },
        restore: { show: true },
        saveAsImage: { show: true }
      },
      iconStyle: {
        borderColor: getThemeColor('gray-700'),
        borderWidth: 1
      },

      emphasis: {
        iconStyle: {
          textFill: getThemeColor('gray-600')
        }
      }
    },
    legend: {
      top: 40,
      data: ['Evaporation', 'Precipitation', 'Average temperature'],
      textStyle: {
        color: getThemeColor('gray-600')
      }
    },
    xAxis: [
      {
        type: 'category',
        data: months,
        axisLabel: {
          color: getThemeColor('gray-600'),
          formatter: value => value.slice(0, 3)
        },
        axisPointer: {
          type: 'shadow'
        },
        axisLine: {
          show: true,
          lineStyle: {
            color: getThemeColor('gray-300')
          }
        }
      }
    ],
    yAxis: [
      {
        type: 'value',
        min: 0,
        max: 250,
        interval: 50,
        axisLabel: {
          color: getThemeColor('gray-600'),
          formatter: '{value} ml'
        },
        splitLine: {
          show: true,
          lineStyle: {
            color: getThemeColor('gray-200')
          }
        }
      },
      {
        type: 'value',
        min: 0,
        max: 25,
        interval: 5,
        axisLabel: {
          color: getThemeColor('gray-600'),
          formatter: '{value} °C'
        },

        splitLine: {
          show: true,
          lineStyle: {
            color: getThemeColor('gray-200')
          }
        }
      }
    ],
    series: [
      {
        name: 'Evaporation',
        type: 'bar',
        data: [2.0, 4.9, 7.0, 23.2, 25.6, 76.7, 135.6, 162.2, 32.6, 20.0, 6.4, 3.3],
        itemStyle: {
          color: getThemeColor('primary'),
          borderRadius: [3, 3, 0, 0]
        }
      },
      {
        name: 'Precipitation',
        type: 'bar',
        data: [2.6, 5.9, 9.0, 26.4, 28.7, 70.7, 175.6, 182.2, 48.7, 18.8, 6.0, 2.3],
        itemStyle: {
          color: getThemeColor('info'),
          borderRadius: [3, 3, 0, 0]
        }
      },
      {
        name: 'Average temperature',
        type: 'line',
        yAxisIndex: 1,
        data: [2.0, 2.2, 3.3, 4.5, 6.3, 10.2, 20.3, 23.4, 23.0, 16.5, 12.0, 6.2],
        lineStyle: {
          color: getThemeColor('warning')
        },
        itemStyle: {
          color: getThemeColor('white'),
          borderColor: getThemeColor('warning'),
          borderWidth: 2
        },
        symbol: 'circle',
        symbolSize: 10
      }
    ],
    grid: {
      right: 5,
      left: 5,
      bottom: 5,
      top: '23%',
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
}`;

const BarLineMixedChart = () => {
  return (
    <OrkestraComponentCard className="h-100">
      <OrkestraComponentCard.Header title="Mixed chart" light={false} />
      <OrkestraComponentCard.Body
        code={chartCode}
        language="jsx"
        scope={{
          ReactEchart,
          echarts,
          tooltipFormatter,
          useAppContext
        }}
      />
    </OrkestraComponentCard>
  );
};

export default BarLineMixedChart;
