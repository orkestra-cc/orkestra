import ReactEchart from 'components/common/ReactEchart';
import dayjs from 'dayjs';
import { HeatmapChart } from 'echarts/charts';
import {
  GridComponent,
  LegendComponent,
  TitleComponent,
  TooltipComponent
} from 'echarts/components';
import * as echarts from 'echarts/core';
import { CanvasRenderer } from 'echarts/renderers';
import { getPastDates, getRandomNumber, rgbaColor } from 'helpers/utils';
import { useAppContext } from 'providers/AppProvider';


echarts.use([
  TitleComponent,
  TooltipComponent,
  GridComponent,
  HeatmapChart,
  CanvasRenderer,
  LegendComponent
]);

const hours = [
  '12 AM',
  '1 AM',
  '2 AM',
  '3 AM',
  '4 AM',
  '5 AM',
  '6 AM',
  '7 AM',
  '8 AM',
  '9 AM',
  '10 AM',
  '11 AM',
  '12 PM',
  '1 PM',
  '2 PM',
  '3 PM',
  '4 PM',
  '5 PM',
  '6 PM',
  '7 PM',
  '8 PM',
  '9 PM',
  '10 PM',
  '11 PM'
];

type ThemeColorGetter = (color: string) => string;
type HeatmapDataPoint = [number, number, number];

interface TooltipParams {
  name: string;
  data: HeatmapDataPoint;
}

const data: HeatmapDataPoint[] = [];
for (let i = 0; i < 24; i += 1) {
  for (let j = 0; j < 7; j += 1) {
    data.push([j, i, getRandomNumber(20, 300)]);
  }
}

const tooltipFormatter = (params: TooltipParams) => {
  return `<div>
        <p class='mb-0 text-600'>${dayjs(params.name).format(
          'MMM DD, YYYY'
        )}</p>
        <div class="d-flex align-items-center">
          <p class="mb-0 text-600">
            ${dayjs()
              .hour(params.data[1])
              .format('hA')} : <span class='text-800 fw-semibold'>${
    params.data[2]
  }</span>
          </p>
        </div>
      </div>`;
};

const getOptions = (getThemeColor: ThemeColorGetter) => ({
  gradientColor: [getThemeColor('info'), getThemeColor('primary')],
  tooltip: {
    position: 'top',
    padding: [7, 10],
    backgroundColor: getThemeColor('gray-100'),
    borderColor: getThemeColor('gray-300'),
    textStyle: { color: getThemeColor('gray-1100') },
    borderWidth: 1,
    formatter: tooltipFormatter
  },
  xAxis: {
    type: 'category',
    data: getPastDates(7),
    splitArea: {
      show: true
    },
    axisTick: { show: false },
    axisLabel: {
      color: getThemeColor('gray-600'),
      formatter: (value: string) => dayjs(value).format('ddd')
    },
    axisLine: {
      lineStyle: {
        color: getThemeColor('gray-400')
      }
    }
  },
  yAxis: {
    position: 'right',
    type: 'category',
    inverse: true,
    data: hours,
    splitArea: {
      show: true
    },
    axisTick: { show: false },
    axisLabel: {
      color: getThemeColor('gray-600'),
      margin: 20,
      padding: [10, 0, 0, 0]
    },
    axisLine: {
      lineStyle: {
        color: getThemeColor('gray-400')
      }
    }
  },
  visualMap: {
    type: 'piecewise',
    orient: 'horizontal',
    left: 'left',
    bottom: '3%',
    itemSymbol: 'circle',
    itemWidth: '10px',
    itemHeight: '10px',
    min: 20,
    max: 300,
    splitNumber: 4,
    textGap: 5,
    textStyle: {
      color: getThemeColor('gray-600'),
      fontWeight: 500,
      lineHeight: 1000000
    }
  },
  series: [
    {
      name: 'Users By Time',
      type: 'heatmap',
      data: data,
      label: {
        show: false
      },
      itemStyle: {
        borderColor: getThemeColor('gray-100'),
        borderWidth: 3
      },
      emphasis: {
        itemStyle: {
          shadowBlur: 3,
          shadowColor: rgbaColor(getThemeColor('black'), 0.5)
        }
      }
    }
  ],
  grid: {
    containLabel: true,
    right: '5px',
    left: 0,
    bottom: '15%',
    top: 0
  }
});

const UsersAtTimeChart = () => {
  const { getThemeColor } = useAppContext();
  return (
    <ReactEchart
      echarts={echarts}
      option={getOptions(getThemeColor)}
      style={{ height: '24.75rem' }}
    />
  );
};

export default UsersAtTimeChart;
