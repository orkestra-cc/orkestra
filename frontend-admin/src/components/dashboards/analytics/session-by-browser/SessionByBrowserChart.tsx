
import { PieChart } from 'echarts/charts';
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
  TitleComponent,
  TooltipComponent,
  GridComponent,
  PieChart,
  CanvasRenderer,
  LegendComponent
]);

const data = [
  { value: 50.3, name: 'Chrome' },
  { value: 20.6, name: 'Safari' },
  { value: 30.1, name: 'Mozilla' }
];

type ThemeColorGetter = (color: string) => string;
type TooltipParams = { data: { name: string; value: number } };

const getOptions = (getThemeColor: ThemeColorGetter) => ({
  color: [
    getThemeColor('primary'),
    getThemeColor('success'),
    getThemeColor('info')
  ],
  tooltip: {
    trigger: 'item',
    padding: [7, 10],
    backgroundColor: getThemeColor('gray-100'),
    borderColor: getThemeColor('gray-300'),
    textStyle: { color: getThemeColor('gray-1100') },
    borderWidth: 1,
    transitionDuration: 0,
    formatter: (params: TooltipParams) =>
      `<strong>${params.data.name}:</strong> ${params.data.value}%`
  },

  legend: { show: false },
  series: [
    {
      type: 'pie',
      radius: ['100%', '65%'],
      avoidLabelOverlap: false,
      itemStyle: {
        borderWidth: 2,
        borderColor: getThemeColor('gray-100')
      },
      label: {
        show: false
      },
      emphasis: {
        scale: false
      },
      data
    }
  ]
});

const SessionByBrowserChart = () => {
  const { getThemeColor } = useAppContext();
  return (
    <ReactEchart
      echarts={echarts}
      option={getOptions(getThemeColor)}
      style={{ height: 200 }}
    />
  );
};

export default SessionByBrowserChart;
