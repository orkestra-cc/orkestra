
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
import { rgbaColor } from 'helpers/utils';
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
  const data = [
    {
      value: 800,
      name: 'Starter',
      itemStyle: {
        color: rgbaColor(getThemeColor('primary'), 0.5)
      }
    },
    {
      value: 1048,
      name: 'Starter Pro',
      itemStyle: {
        color: getThemeColor('danger')
      }
    },
    {
      value: 735,
      name: 'Basic',
      itemStyle: {
        color: getThemeColor('primary')
      }
    },
    {
      value: 580,
      name: 'Optimal',
      itemStyle: {
        color: getThemeColor('secondary')
      }
    },
    {
      value: 484,
      name: 'Business',
      itemStyle: {
        color: getThemeColor('warning')
      }
    },
    {
      value: 600,
      name: 'Classic addition',
      itemStyle: {
        color: rgbaColor(getThemeColor('warning'), 0.8)
      }
    },
    {
      value: 300,
      name: 'Premium',
      itemStyle: {
        color: getThemeColor('success')
      }
    },
    {
      value: 300,
      name: 'Platinum',
      itemStyle: {
        color: getThemeColor('info')
      }
    },
    {
      value: 400,
      name: 'Platinum Pro',
      itemStyle: {
        color: rgbaColor(getThemeColor('primary'), 0.5)
      }
    }
  ];
  
  const getOption = () => ({
    title: [
      {
        text: 'Pie Label Align Chart',
        left: 'center',
        textStyle: {
          color: getThemeColor('gray-600')
        }
      },
      {
        subtext: 'alignTo: "labelLine"',
        left: '50%',
        top: '85%',
        textAlign: 'center',
        subtextStyle: {
          color: getThemeColor('gray-700')
        }
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
    },

    series: [
      {
        type: 'pie',
        radius: window.innerWidth < 530 ? '45%' : '60%',
        center: ['50%', '50%'],
        data: data,
        label: {
          position: 'outer',
          alignTo: 'labelLine',
          bleedMargin: 5,
          color: getThemeColor('gray-700')
        },
        left: '5%',
        right: '5%',
        top: 0,
        bottom: 0
      }
    ]
    });

    //------- Responsive on window resize -------

    const updateDimensions = () => {
      if (window.innerWidth < 530) {
        chartRef.current.getEchartsInstance().setOption({
          series: [{ radius: '45%' }]
        });
      }
      else
        chartRef.current.getEchartsInstance().setOption({
          series: [{ radius: '60%' }]
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

const PieLabelAlignChart = () => {
  return (
    <FalconComponentCard dir="ltr" className="h-100">
      <FalconComponentCard.Header title="Pie chart" light={false} />
      <FalconComponentCard.Body
        code={chartCode}
        language="jsx"
        scope={{
          ReactEchart,
          echarts,
          useAppContext,
          rgbaColor
        }}
      />
    </FalconComponentCard>
  );
};

export default PieLabelAlignChart;
