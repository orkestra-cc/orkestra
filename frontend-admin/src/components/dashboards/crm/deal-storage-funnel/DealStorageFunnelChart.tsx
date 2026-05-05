
import * as echarts from 'echarts/core';
import { useAppContext } from 'providers/AppProvider';
import ReactEchart from 'components/common/ReactEchart';

type ThemeColorGetter = (color: string) => string;

const getOptions = (getThemeColor: ThemeColorGetter) => ({
  yAxis: [
    {
      data: [
        'Processing',
        'Contact won',
        'Contact Sent',
        'Qualified to Buy',
        'Created'
      ],
      axisLabel: {
        inside: true,
        color: getThemeColor('gray-700'),
        fontWeight: 500,
        fontSize: 11,
        fontFamily: 'poppins'
      },
      axisTick: {
        show: false
      },
      axisLine: {
        show: false
      },
      z: 10
    },
    {
      data: ['50%', '70%', '76%', '68%', '99%'],
      axisLabel: {
        inside: false,
        color: getThemeColor('primary'),
        fontWeight: 500,
        fontSize: 11,
        fontFamily: 'poppins',
        borderRadius: 5,
        backgroundColor: getThemeColor('primary-bg-subtle'),
        padding: [6, 16, 6, 16],
        width: 115
      },
      axisTick: {
        show: false
      },
      axisLine: {
        show: false
      },
      z: 10
    }
  ],
  xAxis: {
    type: 'value',
    min: 0,
    max: 35,
    axisLine: {
      show: false
    },
    splitLine: {
      show: false
    },
    inverse: true,
    axisTick: {
      show: false
    },
    axisLabel: {
      show: false
    }
  },

  series: [
    {
      type: 'bar',
      showBackground: true,
      barWidth: 25,
      label: {
        show: true,
        formatter: '{c} ',
        position: 'insideLeft'
      },
      backgroundStyle: {
        color: getThemeColor('gray-200'),
        borderRadius: 5
      },
      itemStyle: {
        color: getThemeColor('primary'),
        borderRadius: 5
      },
      data: [7, 10, 13, 19, 19]
    }
  ],
  grid: { right: '65px', left: '0', bottom: '0', top: '0' }
});

const DealStorageFunnelChart = () => {
  const { getThemeColor } = useAppContext();
  return (
    <ReactEchart
      echarts={echarts}
      option={getOptions(getThemeColor)}
      style={{ height: '10rem' }}
    />
  );
};

export default DealStorageFunnelChart;
