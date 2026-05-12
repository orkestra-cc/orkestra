import CardDropdown from 'components/common/CardDropdown';
import OrkestraCardHeader from 'components/common/OrkestraCardHeader';
import Flex from 'components/common/Flex';
import { BarChart } from 'echarts/charts';
import {
  DatasetComponent,
  GridComponent,
  LegendComponent,
  TitleComponent,
  TooltipComponent
} from 'echarts/components';
import * as echarts from 'echarts/core';
import { CanvasRenderer } from 'echarts/renderers';
import { Card } from 'react-bootstrap';
import { Link } from 'react-router';
import { useAppContext } from 'providers/AppProvider';
import ReactEchart from 'components/common/ReactEchart';

echarts.use([
  TitleComponent,
  TooltipComponent,
  GridComponent,
  BarChart,
  CanvasRenderer,
  LegendComponent,
  DatasetComponent
]);

type ThemeColorGetter = (color: string) => string;
type TooltipParams = {
  seriesName: string;
  name: string;
  value: (string | number)[];
  componentIndex: number;
};

const getOption = (
  getThemeColor: ThemeColorGetter,
  data: (string | number)[][]
) => ({
  color: [getThemeColor('primary'), getThemeColor('gray-300')],
  dataset: { source: data },
  tooltip: {
    trigger: 'item',
    padding: [7, 10],
    backgroundColor: getThemeColor('gray-100'),
    borderColor: getThemeColor('gray-300'),
    textStyle: { color: getThemeColor('gray-1100') },
    borderWidth: 1,
    transitionDuration: 0,
    formatter: function (params: TooltipParams) {
      return `<div className="fw-semibold">${
        params.seriesName
      }</div><div className="fs-10 text-600"><strong>${params.name}:</strong> ${
        params.value[params.componentIndex + 1]
      }</div>`;
    }
  },
  legend: {
    data: ['2019', '2018'],
    left: 'left',
    itemWidth: 10,
    itemHeight: 10,
    borderRadius: 0,
    icon: 'circle',
    inactiveColor: getThemeColor('gray-400'),
    textStyle: { color: getThemeColor('gray-700') }
  },
  xAxis: {
    type: 'category',
    axisLabel: { color: getThemeColor('gray-400') },
    axisLine: {
      lineStyle: {
        color: getThemeColor('gray-300'),
        type: 'dashed'
      }
    },
    axisTick: false,
    boundaryGap: true
  },
  yAxis: {
    axisPointer: { type: 'none' },
    axisTick: 'none',
    splitLine: {
      lineStyle: {
        color: getThemeColor('gray-300'),
        type: 'dashed'
      }
    },
    axisLine: { show: false },
    axisLabel: { color: getThemeColor('gray-400') }
  },
  series: [
    {
      type: 'bar',
      barWidth: '10px',
      barGap: '30%',
      label: { show: false },
      z: 10,
      itemStyle: {
        borderRadius: [10, 10, 0, 0],
        color: getThemeColor('primary')
      }
    },
    {
      type: 'bar',
      barWidth: '10px',
      barGap: '30%',
      label: { show: false },
      itemStyle: {
        borderRadius: [10, 10, 0, 0],
        color: getThemeColor('gray-300')
      }
    }
  ],
  grid: { right: '0', left: '30px', bottom: '10%', top: '20%' }
});

interface TopProductsProps {
  data: (string | number)[][];
  className?: string;
}

const TopProducts = ({ data, className }: TopProductsProps) => {
  const { getThemeColor } = useAppContext();
  return (
    <Card className={className || 'h-100'}>
      <OrkestraCardHeader
        title="Top Products"
        titleTag="h6"
        className="py-2"
        light
        endEl={
          <Flex>
            <Link to="#!" className="btn btn-link btn-sm me-2">
              View Details
            </Link>
            <CardDropdown />
          </Flex>
        }
      />
      <Card.Body className="h-100">
        <ReactEchart
          echarts={echarts}
          option={getOption(getThemeColor, data)}
          style={{ height: '100%', minHeight: '17.75rem' }}
        />
      </Card.Body>
    </Card>
  );
};

export default TopProducts;
