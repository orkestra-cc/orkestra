
import OrkestraComponentCard from 'components/common/OrkestraComponentCard';
import { LineChart, PieChart } from 'echarts/charts';
import {
  GridComponent,
  TitleComponent,
  TooltipComponent,
  VisualMapComponent
} from 'echarts/components';
import * as echarts from 'echarts/core';
import { CanvasRenderer } from 'echarts/renderers';
import { useAppContext } from 'providers/AppProvider';
import ReactEchart from 'components/common/ReactEchart';

echarts.use([
  TitleComponent,
  TooltipComponent,
  GridComponent,
  VisualMapComponent,
  LineChart,
  PieChart,
  CanvasRenderer
]);

const chartCode = `function ChartOptions() {
  const { getThemeColor } = useAppContext();
  const chartRef = useRef(null);

  const getOption = () => ({
    color: [
      getThemeColor('danger'),
      getThemeColor('warning'),
      getThemeColor('info'),
      getThemeColor('primary')
    ],
    legend: {
      top: 0,
      textStyle: {
        color: getThemeColor('gray-700')
      }
    },
    tooltip: {
      trigger: 'axis',
      showContent: false
    },
    dataset: {
      source: [
        ['product', '2012', '2013', '2014', '2015', '2016', '2017'],
        ['Milk Tea', 56.5, 82.1, 88.7, 70.1, 53.4, 85.1],
        ['Matcha Latte', 51.1, 51.4, 55.1, 53.3, 73.8, 68.7],
        ['Cheese Cocoa', 40.1, 62.2, 69.5, 36.4, 45.2, 32.5],
        ['Walnut Brownie', 25.2, 37.1, 41.2, 18, 33.9, 49.1]
      ]
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
      axisPointer: {
        lineStyle: {
          color: getThemeColor('gray-300')
        }
      }
    },
    yAxis: {
      gridIndex: 0,
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
        type: 'line',
        smooth: true,
        seriesLayoutBy: 'row',
        emphasis: { focus: 'series' },
        symbolSize: 10,
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
        type: 'line',
        smooth: true,
        seriesLayoutBy: 'row',
        emphasis: { focus: 'series' },
        symbolSize: 10,
        itemStyle: {
          color: getThemeColor('white'),
          borderColor: getThemeColor('info'),
          borderWidth: 2
        },
        lineStyle: {
          color: getThemeColor('info')
        },
        symbol: 'circle'
      },
      {
        type: 'line',
        smooth: true,
        seriesLayoutBy: 'row',
        emphasis: { focus: 'series' },
        symbolSize: 10,
        itemStyle: {
          color: getThemeColor('white'),
          borderColor: getThemeColor('warning'),
          borderWidth: 2
        },
        lineStyle: {
          color: getThemeColor('warning')
        },
        symbol: 'circle'
      },
      {
        type: 'line',
        smooth: true,
        seriesLayoutBy: 'row',
        emphasis: { focus: 'series' },
        symbolSize: 10,
        itemStyle: {
          color: getThemeColor('white'),
          borderColor: getThemeColor('primary'),
          borderWidth: 2
        },
        lineStyle: {
          color: getThemeColor('primary')
        },
        symbol: 'circle'
      },
      {
        type: 'pie',
        id: 'pie',
        radius: '30%',
        center: ['50%', '28%'],
        emphasis: { focus: 'data' },
        label: {
          formatter: '{b}: {@2012} ({d}%)',
          color: getThemeColor('gray-600')
        },
        encode: {
          itemName: 'product',
          value: '2012',
          tooltip: '2012'
        }
      }
    ],
    grid: {
      right: 10,
      left: 5,
      bottom: 5,
      top: '55%',
      containLabel: true
    }
  });

  const updateAxisPointer = event => {
    var xAxisInfo = event.axesInfo[0];
    if (xAxisInfo) {
      var dimension = xAxisInfo.value + 1;

      chartRef.current.getEchartsInstance().setOption({
        series: {
          id: 'pie',
          label: {
            formatter: '{b}: {@[' + dimension + ']} ({d}%)'
          },
          encode: {
            value: dimension,
            tooltip: dimension
          }
        }
      });
    }
  };

  const onEvents = {
    updateAxisPointer: updateAxisPointer
  };
  return (
    <ReactEchart
      echarts={echarts}
      ref={chartRef}
      option={getOption()}
      style={{ height: '31.25rem' }}
      onEvents={onEvents}
    />
  )

}`;

const LineShareDatasetChart = () => {
  return (
    <OrkestraComponentCard>
      <OrkestraComponentCard.Header title="Share Dataset" light={false} />
      <OrkestraComponentCard.Body
        code={chartCode}
        language="jsx"
        scope={{
          ReactEchart,
          echarts,
          useAppContext
        }}
      />
    </OrkestraComponentCard>
  );
};

export default LineShareDatasetChart;
