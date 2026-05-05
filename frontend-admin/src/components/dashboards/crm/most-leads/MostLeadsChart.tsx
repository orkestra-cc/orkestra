import { useRef } from 'react';
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

type ThemeColorGetter = (color: string) => string;
type TooltipParams = { data: { name: string }; percent: number };

const getOptions = (getThemeColor: ThemeColorGetter) => ({
  color: [
    getThemeColor('primary'),
    getThemeColor('info'),
    getThemeColor('warning'),
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
    formatter: function (params: TooltipParams) {
      return `<strong>${params.data.name}:</strong> ${params.percent}%`;
    }
  },
  legend: { show: false },
  series: [
    {
      name: 'Total',
      type: 'pie',
      radius: ['100%', '67%'],
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
      labelLine: { show: false },
      data: [
        { value: 60, name: 'Email' },
        { value: 30, name: 'Social' },
        { value: 10, name: 'Call' },
        { value: 120, name: 'Other' }
      ]
    }
  ]
});

const MostLeadsChart = () => {
  const chartRef = useRef(null);
  const { getThemeColor } = useAppContext();

  return (
    <div className="position-relative py-2">
      <ReactEchart
        ref={chartRef}
        echarts={echarts}
        option={getOptions(getThemeColor)}
        style={{ height: '12.5rem' }}
      />
      <div className="position-absolute top-50 start-50 translate-middle text-center">
        <p className="fs-10 mb-0 text-400 font-sans-serif fw-medium">Total</p>
        <p className="fs-6 mb-0 font-sans-serif fw-medium mt-n2">15.6k</p>
      </div>
    </div>
  );
};

export default MostLeadsChart;
