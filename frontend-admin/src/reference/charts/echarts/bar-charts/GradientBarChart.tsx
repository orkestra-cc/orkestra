
import OrkestraComponentCard from 'components/common/OrkestraComponentCard';

import { BarChart, LineChart } from 'echarts/charts';
import {
  GridComponent,
  LegendComponent,
  TitleComponent,
  TooltipComponent,
  DataZoomInsideComponent
} from 'echarts/components';
import * as echarts from 'echarts/core';
import { CanvasRenderer } from 'echarts/renderers';
import { useAppContext } from 'providers/AppProvider';
import ReactEchart from 'components/common/ReactEchart';
echarts.use([
  BarChart,
  LineChart,
  TitleComponent,
  TooltipComponent,
  GridComponent,
  CanvasRenderer,
  DataZoomInsideComponent,
  LegendComponent
]);

const chartCode = `function ChartOptions() {
  const { getThemeColor } = useAppContext();
  var dataAxis = [
    'A',
    'B',
    'C',
    'D',
    'E',
    'F',
    'G',
    'H',
    'I',
    'J',
    'K',
    'L',
    'M',
    'N',
    'O',
    'P',
    'Q',
    'R',
    'S',
    'T'
  ];

  var data = [
    220, 182, 191, 234, 290, 330, 310, 123, 442, 321, 90, 149, 210, 122, 133,
    334, 198, 123, 125, 220
  ];

  const chartRef = useRef(null);

  const tooltipFormatter = params => {
    return (
      ' <div> <h6 class="fs-10 text-700 mb-0">  <div class="dot me-1 fs-11 d-inline-block bg-primary" ></div>' +
      params[0].name +
      ' : ' +
      params[0].value +
      ' </h6> </div> '
    );
  };

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
    title: {
      text: 'Gradient and Clickable bar chart',
      textStyle: {
        color: getThemeColor('gray-600')
      },
      left: 'center'
    },
    xAxis: {
      data: dataAxis,
      axisLabel: {
        inside: true,
        color: '#fff'
      },
      axisTick: {
        show: false
      },
      axisLine: {
        show: false
      },
      z: 10
    },
    yAxis: {
      axisLine: {
        show: false
      },
      axisTick: {
        show: false
      },
      axisLabel: {
        color: getThemeColor('gray-600')
      },
      splitLine: {
        show: true,
        lineStyle: {
          color: getThemeColor('gray-300')
        }
      }
    },
    dataZoom: [
      {
        type: 'inside'
      }
    ],
    series: [
      {
        type: 'bar',
        name: 'Total',
        showBackground: true,
        backgroundStyle: {
          color: getThemeColor('gray-200'),
          opacity: 0.5
        },
        itemStyle: {
          color: new echarts.graphic.LinearGradient(0, 0, 0, 1, [
            { offset: 0, color: getThemeColor('info') },
            { offset: 0.5, color: getThemeColor('primary') },
            { offset: 1, color: getThemeColor('primary') }
          ]),
          borderRadius: [3, 3, 0, 0]
        },
        emphasis: {
          itemStyle: {
            color: new echarts.graphic.LinearGradient(0, 0, 0, 1, [
              { offset: 0, color: getThemeColor('primary') },
              { offset: 0.7, color: getThemeColor('primary') },
              { offset: 1, color: getThemeColor('info') }
            ])
          }
        },
        data: data
      }
    ],
    grid: {
      right: 5,
      left: 5,
      bottom: 5,
      top: '10%',
      containLabel: true
    }
  });
  
  const zoomSize = 6;
  const onEvents = {
    click: params => {
      chartRef.current.getEchartsInstance().dispatchAction({
        type: 'dataZoom',
        startValue: dataAxis[Math.max(params.dataIndex - zoomSize / 2, 0)],
        endValue:
          dataAxis[Math.min(params.dataIndex + zoomSize / 2, data.length - 1)]
      });
    }
  };
  
  return (
    <ReactEchart
      echarts={echarts}
      ref={chartRef}
      option={getOption()}
      style={{ height: '21.88rem' }}
      onEvents={onEvents}
    />
  );
}`;

const GradientBarChart = () => {
  return (
    <OrkestraComponentCard className="h-100">
      <OrkestraComponentCard.Header title="Gradient bar chart" light={false} />
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

export default GradientBarChart;
