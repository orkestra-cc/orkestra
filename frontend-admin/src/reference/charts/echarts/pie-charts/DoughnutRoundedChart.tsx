
import FalconComponentCard from 'components/common/FalconComponentCard';

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
  const chartRef = useRef(null);
  
  const getOption = () => ({
    legend: {
      orient: 'vertical',
      left: 'left',
      textStyle: {
        color: getThemeColor('gray-600')
      }
    },
    series: [
      {
        type: 'pie',
        radius: ['40%', '70%'],
        center: window.innerWidth < 530 ? ['65%', '55%'] : ['50%', '55%'],
        avoidLabelOverlap: false,
        itemStyle: {
          borderRadius: 10,
          borderColor: getThemeColor('gray-100'),
          borderWidth: 2
        },
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
            name: 'Starter',
            itemStyle: {
              color: getThemeColor('primary')
            }
          },
          {
            value: 735,
            name: 'Basic',
            itemStyle: {
              color: getThemeColor('danger')
            }
          },
          {
            value: 580,
            name: 'Optimal',
            itemStyle: {
              color: getThemeColor('info')
            }
          },
          {
            value: 484,
            name: 'Business',
            itemStyle: {
              color: getThemeColor('success')
            }
          },
          {
            value: 300,
            name: 'Premium',
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

    //------- Responsive on window resize -------
    
    const updateDimensions = () => {
      if (window.innerWidth < 530) {
        chartRef.current.getEchartsInstance().setOption({
          series: [
            {
              center: ['65%', '55%']
            }
          ]
        });
      } 
      else
        chartRef.current.getEchartsInstance().setOption({
          series: [
            {
              center: ['50%', '55%']
            }
          ]
        });
    }
  
    useEffect(() => {
      window.addEventListener('resize', updateDimensions, { passive: true });
      return () => window.removeEventListener('resize', updateDimensions);
    }, []);

    return (
      <ReactEchart
        echarts={echarts}
        option={getOption()}
        ref={chartRef}
        style={{ height: '20rem' }}
      />
    );
  }
`;

const DoughnutRoundedChart = () => {
  return (
    <FalconComponentCard dir="ltr" className="h-100">
      <FalconComponentCard.Header
        title="Doughnut rounded chart"
        light={false}
      />
      <FalconComponentCard.Body
        code={chartCode}
        language="jsx"
        scope={{
          ReactEchart,
          echarts,
          useAppContext
        }}
      />
    </FalconComponentCard>
  );
};

export default DoughnutRoundedChart;
