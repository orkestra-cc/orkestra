
import { rgbaColor } from 'helpers/utils';
import * as echarts from 'echarts/core';
import {
  GridComponent,
  LegendComponent,
  TitleComponent,
  TooltipComponent
} from 'echarts/components';
import { PieChart } from 'echarts/charts';
import { CanvasRenderer } from 'echarts/renderers';
import { useAppContext } from 'providers/AppProvider';
import ReactEchart from 'components/common/ReactEchart';

echarts.use([
  TitleComponent,
  TooltipComponent,
  GridComponent,
  PieChart,
  CanvasRenderer,
  LegendComponent
]);

type ThemeColorGetter = (color: string) => string;

const getOption = (getThemeColor: ThemeColorGetter, isDark: boolean) => ({
  legend: {
    left: 'center',
    bottom: 22,
    itemWidth: 12,
    itemHeight: 12,
    borderRadius: 0,
    icon: 'circle',
    inactiveColor: getThemeColor('gray-400'),
    inactiveBorderColor: 'transparent',
    textStyle: {
      color: getThemeColor('gray-600'),
      fontSize: 12,
      fontFamily: 'Poppins',
      fontWeight: '500'
    },
    itemGap: 16
  },
  series: [
    {
      type: 'pie',
      radius: '70%',
      label: {
        show: false
      },
      center: ['50%', '45%'],
      itemStyle: {
        borderWidth: 2,
        borderColor: isDark ? '#121E2D' : getThemeColor('gray-100')
      },
      data: [
        {
          value: 1100,
          name: 'Positive',
          itemStyle: {
            color: getThemeColor('primary')
          }
        },
        {
          value: 550,
          name: 'Nagative',
          itemStyle: {
            color: rgbaColor(getThemeColor('primary'), 0.5)
          }
        }
      ]
    }
  ],
  tooltip: {
    trigger: 'item',
    padding: [7, 10],
    backgroundColor: getThemeColor('gray-100'),
    borderColor: getThemeColor('gray-300'),
    textStyle: { color: getThemeColor('gray-1100') },
    borderWidth: 1,
    transitionDuration: 0,
    axisPointer: {
      type: 'none'
    }
  }
});

const CustomerSatisfactionChart = () => {
  const { config, getThemeColor } = useAppContext();
  const { isDark } = config;
  return (
    <ReactEchart
      echarts={echarts}
      option={getOption(getThemeColor, isDark)}
      className="h-100 w-100"
    />
  );
};

export default CustomerSatisfactionChart;
