
import merge from 'lodash.merge';
import { useAppContext } from 'providers/AppProvider';
import ReactEchart from './ReactEchart';

interface BasicEChartsProps {
  echarts: any;
  options: any;
  [key: string]: any;
}

const getOption = (getThemeColor: (color: string) => string): any => ({
  color: getThemeColor('primary'),
  tooltip: {
    trigger: 'item',
    axisPointer: {
      type: 'none'
    },
    padding: [7, 10],
    backgroundColor: getThemeColor('gray-100'),
    borderColor: getThemeColor('gray-100'),
    textStyle: { color: getThemeColor('gray-1100') },
    borderWidth: 1,
    transitionDuration: 0
  },
  xAxis: {
    type: 'category',
    show: false,
    boundaryGap: false
  },
  yAxis: {
    show: false,
    type: 'value',
    boundaryGap: false
  },
  series: [
    {
      type: 'bar',
      symbol: 'none'
    }
  ],
  grid: { right: '0', left: '0', bottom: '0', top: '0' }
});

const BasicECharts: React.FC<BasicEChartsProps> = ({ echarts, options, ...rest }) => {
  const { getThemeColor } = useAppContext();
  return (
    <ReactEchart
      ref={undefined}
      echarts={echarts}
      option={merge(getOption(getThemeColor), options)}
      {...rest}
    />
  );
};

export default BasicECharts;
