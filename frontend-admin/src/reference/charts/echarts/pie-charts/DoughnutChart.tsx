
import OrkestraComponentCard from 'components/common/OrkestraComponentCard';

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

const chartCode = `function ChartOptions() {
  const { getThemeColor } = useAppContext();
  
  const getOption = () => ({
    legend: {
      left: 'left',
      textStyle: {
        color: getThemeColor('gray-600')
      }
    },
    series: [
      {
        type: 'pie',
        radius: ['40%', '70%'],
        center: ['50%', '55%'],
        avoidLabelOverlap: false,
        label: {
          show: false,
          position: 'center'
        },
        labelLine: {
          show: false
        },
        data: [
          {
            value: 1048,
            name: 'Facebook',
            itemStyle: {
              color: getThemeColor('primary')
            }
          },
          {
            value: 735,
            name: 'Youtube',
            itemStyle: {
              color: getThemeColor('danger')
            }
          },
          {
            value: 580,
            name: 'Twitter',
            itemStyle: {
              color: getThemeColor('info')
            }
          },
          {
            value: 484,
            name: 'Linkedin',
            itemStyle: {
              color: getThemeColor('success')
            }
          },
          {
            value: 300,
            name: 'Github',
            itemStyle: {
              color: getThemeColor('warning')
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
    
    return (
      <ReactEchart
        echarts={echarts}
        option={getOption()}
        style={{ height: '20rem' }}
      />
    );
  }
`;

const DoughnutChart = () => {
  return (
    <OrkestraComponentCard dir="ltr" className="h-100">
      <OrkestraComponentCard.Header title="Doughnut chart" light={false} />
      <OrkestraComponentCard.Body
        code={chartCode}
        language="jsx"
        scope={{
          ReactEchart,
          echarts,
          useAppContext
        }}
      />
    </OrkestraComponentCard>
  );
};

export default DoughnutChart;
