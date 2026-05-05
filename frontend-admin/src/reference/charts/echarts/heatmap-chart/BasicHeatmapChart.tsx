
import FalconComponentCard from 'components/common/FalconComponentCard';

import { HeatmapChart } from 'echarts/charts';
import {
  GridComponent,
  LegendComponent,
  TitleComponent,
  TooltipComponent,
  VisualMapComponent
} from 'echarts/components';
import * as echarts from 'echarts/core';
import { CanvasRenderer } from 'echarts/renderers';
import { getRandomNumber, rgbaColor } from 'helpers/utils';
import { useAppContext } from 'providers/AppProvider';
import ReactEchart from 'components/common/ReactEchart';
echarts.use([
  TitleComponent,
  TooltipComponent,
  VisualMapComponent,
  GridComponent,
  HeatmapChart,
  CanvasRenderer,
  LegendComponent
]);

const chartCode = `function ChartOptions() {
  const { getThemeColor } = useAppContext();

  const hours = ['12a', '2a', '4a', '6a', '8a', '10a', '12p', '2p', '4p', '6p', '8p', '10p'];
  const days = ['Monday', 'Tuesday', 'Wednesday', 'Thursday', 'Friday', 'Saturday', 'Sunday'];

  const data = [];
  for (let i = 0; i < 7; i += 1) {
    for (let j = 0; j < 12; j += 1) {
      data.push([j, i, getRandomNumber(5, 12)]);
    }
  }

  const getOption = () => ({
    tooltip: {
      position: 'top',
      padding: [7, 10],
      backgroundColor: getThemeColor('gray-100'),
      borderColor: getThemeColor('gray-300'),
      textStyle: { color: getThemeColor('gray-1100') },
      borderWidth: 1
    },
    grid: {
      right: 5,
      left: 5,
      top: 5,
      bottom: '15%',
      containLabel: true
    },
    xAxis: {
      type: 'category',
      data: hours,
      splitArea: {
        show: true
      },
      axisLabel: {
        color: getThemeColor('gray-600')
      },
      axisLine: {
        show: true,
        lineStyle: {
          color: getThemeColor('gray-400')
        }
      }
    },
    yAxis: {
      type: 'category',
      data: days,
      axisLabel: {
        formatter: value => value.substring(0, 3),
        color: getThemeColor('gray-600')
      },
      splitArea: {
        show: true
      },
      axisLine: {
        show: true,
        lineStyle: {
          color: getThemeColor('gray-400')
        }
      }
    },
    visualMap: {
      min: 0,
      max: 10,
      calculable: true,
      orient: 'horizontal',
      left: 'center',
      bottom: '0%',
      textStyle: {
        color: getThemeColor('gray-600'),
        fontWeight: 500
      },
      inRange: {
        color: [
          rgbaColor(getThemeColor('primary'), 1),
          rgbaColor(getThemeColor('info'), 1),
          rgbaColor(getThemeColor('success'), 1)
        ]
      }
    },
    series: [
      {
        type: 'heatmap',
        data: data,
        label: {
          show: true
        },
        emphasis: {
          itemStyle: {
            shadowBlur: 10,
            shadowColor: rgbaColor(getThemeColor('black'), 0.5)
          }
        }
      }
    ]
    });
    return (
      <ReactEchart
        echarts={echarts}
        option={getOption()}
        style={{ height: '21.88rem' }}
      />
    );
  }
`;

const BasicHeatmapChart = () => {
  return (
    <FalconComponentCard dir="ltr" className="h-100">
      <FalconComponentCard.Header title="Heatmap chart" light={false} />
      <FalconComponentCard.Body
        code={chartCode}
        language="jsx"
        scope={{
          ReactEchart,
          echarts,
          useAppContext,
          rgbaColor,
          getRandomNumber
        }}
      />
    </FalconComponentCard>
  );
};

export default BasicHeatmapChart;
