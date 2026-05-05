import ReactEchart from 'components/common/ReactEchart';
import { LineChart } from 'echarts/charts';
import {
  GridComponent,
  LegendComponent,
  TitleComponent,
  TooltipComponent
} from 'echarts/components';
import * as echarts from 'echarts/core';
import { CanvasRenderer } from 'echarts/renderers';
import { getRandomNumber, rgbaColor } from 'helpers/utils';
import { useAppContext } from 'providers/AppProvider';
import { useEffect, useRef } from 'react';
import ReactEChartsCore from 'echarts-for-react/lib/core';

echarts.use([
  TitleComponent,
  TooltipComponent,
  GridComponent,
  LineChart,
  CanvasRenderer,
  LegendComponent
]);

const data = [
  921, 950, 916, 913, 909, 962, 926, 936, 977, 976, 999, 981, 998, 1000, 900,
  906, 973, 911, 994, 982, 917, 972, 952, 963, 991
];
const axisData = Array.from(Array(25).keys());

type ThemeColorGetter = (color: string) => string;
type TooltipParams = { value: number }[];

const getOptions = (getThemeColor: ThemeColorGetter) => ({
  tooltip: {
    trigger: 'axis',
    padding: [7, 10],
    axisPointer: {
      type: 'none'
    },
    backgroundColor: getThemeColor('gray-100'),
    borderColor: getThemeColor('gray-300'),
    textStyle: { color: getThemeColor('gray-1100') },
    borderWidth: 1,
    transitionDuration: 0,
    formatter: (params: TooltipParams) => {
      return `
        <div>
          <h6 class="fs-10 text-700 mb-0 d-flex align-items-center">
          <div class="dot me-1" style="background-color:${getThemeColor(
            'primary'
          )}"></div>
            Users : ${params[0].value}
          </h6>
        </div>
      `;
    }
  },
  xAxis: {
    type: 'category',

    axisLabel: {
      show: false
    },
    axisTick: {
      show: false
    },
    axisLine: {
      show: false
    },
    boundaryGap: [0.2, 0.2],
    data: axisData
  },
  yAxis: {
    type: 'value',
    scale: true,
    boundaryGap: false,
    axisLabel: {
      show: false
    },
    splitLine: {
      show: false
    },
    min: 500,
    max: 1100
  },
  series: [
    {
      type: 'bar',
      barCategoryGap: '12%',
      data,
      itemStyle: {
        color: rgbaColor('#fff', 0.3)
      }
    }
  ],
  grid: { right: '0px', left: '0px', bottom: 0, top: 0 }
});

interface RealTimeUsersChartProps {
  setUserCount: (count: number) => void;
}

const RealTimeUsersChart = ({ setUserCount }: RealTimeUsersChartProps) => {
  const chartRef = useRef<ReactEChartsCore>(null);
  const { getThemeColor } = useAppContext();

  useEffect(() => {
    const interval = setInterval(() => {
      if (chartRef.current?.getEchartsInstance) {
        const rndData = getRandomNumber(900, 1000);
        data.shift();
        data.push(rndData);
        axisData.shift();
        axisData.push(getRandomNumber(100, 500));

        setUserCount(rndData);

        chartRef.current.getEchartsInstance().setOption({
          xAxis: {
            data: axisData
          },
          series: [
            {
              data
            }
          ]
        });
      }
    }, 5000); // Reduced frequency from 2s to 5s for better performance
    return () => {
      clearInterval(interval);
    };
  }, [setUserCount]);

  return (
    <ReactEchart
      ref={chartRef}
      echarts={echarts}
      option={getOptions(getThemeColor)}
      style={{ height: '9.375rem' }}
    />
  );
};

export default RealTimeUsersChart;
