import ReactEchart from 'components/common/ReactEchart';
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
import { getPastDates, rgbaColor } from 'helpers/utils';
import { useAppContext } from 'providers/AppProvider';
import { useRef } from 'react';

echarts.use([
  TitleComponent,
  TooltipComponent,
  GridComponent,
  LineChart,
  CanvasRenderer,
  LegendComponent
]);

type ThemeColorGetter = (color: string) => string;
type ChartTooltipParams = {
  value: number;
  axisValue: string;
  data: number;
}[];

const getOptions = (getThemeColor: ThemeColorGetter, data: number[][]) => ({
  color: getThemeColor('gray-100'),
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
    formatter: (params: ChartTooltipParams) => {
      const percentage =
        ((params[0].value - params[1].value) / params[1].value) * 100;
      const perTemp = `
        <div class="d-flex align-items-center ms-2">
          <h6 class="fs-11 mb-0 ms-1 fw-semibold"> 
            <span style="color:${
              percentage < 0
                ? getThemeColor('danger')
                : getThemeColor('success')
            };">${percentage < 0 ? '&#9660' : '&#9650'}</span> 
            ${Math.abs(percentage).toFixed(2)} %</h6>
        </div>
      `;

      const currentDate = new Date(params[0].axisValue);
      const prevDate = new Date(new Date().setDate(currentDate.getDate() - 7));
      return `<div>
            <p class='mb-0 fs-11 text-600'>${dayjs(params[0].axisValue).format(
              'MMM DD'
            )} vs ${dayjs(prevDate).format('MMM DD')}</p>
            <div class="d-flex align-items-center">
              <p class='mb-0 text-600 fs-10'>
                Users: <span class='text-800 fw-semibold fs-10'>${
                  params[0].data
                }</span>
              </p>
              ${perTemp}
            </div>
          </div>`;
    }
  },
  xAxis: {
    type: 'category',
    data: getPastDates(7),
    axisLabel: {
      color: getThemeColor('gray-600'),
      formatter: (value: string) => dayjs(value).format('MMM DD'),
      align: 'left',
      fontSize: 11,
      padding: [0, 0, 0, 5],
      showMaxLabel: false
    },
    axisLine: {
      lineStyle: {
        color: getThemeColor('gray-200')
      }
    },
    axisTick: {
      show: true,
      length: 20,
      lineStyle: {
        color: getThemeColor('gray-200')
      }
    },
    boundaryGap: false
  },
  yAxis: {
    position: 'right',
    axisPointer: { type: 'none' },
    axisTick: 'none',
    splitLine: {
      lineStyle: {
        color: getThemeColor('gray-200')
      }
    },
    axisLine: { show: false },
    axisLabel: { color: getThemeColor('gray-600') }
  },
  series: [
    {
      type: 'line',
      data: data[0],
      showSymbol: false,
      symbol: 'circle',
      animation: false,
      itemStyle: {
        borderColor: getThemeColor('primary'),
        borderWidth: 2
      },
      lineStyle: {
        color: getThemeColor('primary')
      },
      areaStyle: {
        color: {
          type: 'linear',
          x: 0,
          y: 0,
          x2: 0,
          y2: 1,
          colorStops: [
            {
              offset: 0,
              color: rgbaColor(getThemeColor('primary'), 0.2)
            },
            {
              offset: 1,
              color: rgbaColor(getThemeColor('primary'), 0)
            }
          ]
        }
      }
    },
    {
      type: 'line',
      data: data[1],
      symbol: 'none',
      lineStyle: {
        type: 'dashed',
        width: 1,
        color: getThemeColor('info')
      }
    }
  ],
  grid: {
    containLabel: true,
    right: '5px',
    left: 0,
    bottom: 0,
    top: '10px'
  }
});

interface AudienceChartProps {
  data: number[][];
}

const AudienceChart = ({ data }: AudienceChartProps) => {
  const chartRef = useRef(null);
  const { getThemeColor } = useAppContext();

  return (
    <ReactEchart
      ref={chartRef}
      echarts={echarts}
      option={getOptions(getThemeColor, data)}
      style={{ height: '21.25rem' }}
    />
  );
};

export default AudienceChart;
