
import OrkestraComponentCard from 'components/common/OrkestraComponentCard';

import { RadarChart } from 'echarts/charts';
import {
  GridComponent,
  LegendComponent,
  TitleComponent,
  TooltipComponent
} from 'echarts/components';
import * as echarts from 'echarts/core';
import { CanvasRenderer } from 'echarts/renderers';
import { rgbaColor } from 'helpers/utils';
import { useBreakpoints } from 'hooks/ui/useBreakpoints';
import { useAppContext } from 'providers/AppProvider';
import ReactEchart from 'components/common/ReactEchart';

echarts.use([
  TitleComponent,
  TooltipComponent,
  GridComponent,
  RadarChart,
  CanvasRenderer,
  LegendComponent
]);

function tooltipFormatter(params: any) {
  const indicators = [
    ['Marketing', 'Sales', 'Dev', 'Support', 'Tech', 'Admin'],
    ['Language', 'Math', 'English', 'Physics', 'Chemistry', 'Biology']
  ];
  const num = params.seriesIndex;
  return `<strong > ${params.name} </strong>
  <div class="fs-10 text-600">
    <strong >${indicators[params.seriesIndex][0]}</strong>: ${
    params.value[0]
  }  <br>
    <strong>${indicators[num][1]}</strong>: ${params.value[1]}  <br>
    <strong>${indicators[num][2]}</strong>: ${params.value[2]}  <br>
    <strong>${indicators[num][3]}</strong>: ${params.value[3]}  <br>
    <strong>${indicators[num][4]}</strong>: ${params.value[4]}  <br>
    <strong>${indicators[num][5]}</strong>: ${params.value[5]}  <br>
  </div>`;
}

const chartCode = `function ChartOptions() {
  const { getThemeColor } = useAppContext();
  const chartRef = useRef(null);
  const { breakpoints } = useBreakpoints();

  const months = [
    'January',
    'February',
    'March',
    'April',
    'May',
    'June',
    'July',
    'August',
    'September',
    'October',
    'November',
    'December'
  ];

  const getCenter = () => {
    if (window.innerWidth < 1540 && window.innerWidth > 992) {
      return [
        ['25%', '40%'],
        ['50%', '75%'],
        ['75%', '40%']
      ];
    } else if (window.innerWidth < 992) {
      return [
        ['50%', '20%'],
        ['50%', '50%'],
        ['50%', '80%']
      ];
    } else {
      return [
        ['15%', '50%'],
        ['50%', '50%'],
        ['85%', '50%']
      ];
    }
  };

  const getOption = () => ({
    legend: {
      left: 'left',
      textStyle: {
        color: getThemeColor('gray-600')
      }
    },
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

    radar: [
      {
        indicator: [
          { text: 'Brand', max: 100 },
          { text: 'content', max: 100 },
          { text: 'Usability', max: 100 },
          { text: 'Features', max: 100 }
        ],
        center: getCenter()[0],
        radius: 85,
        splitLine: {
          lineStyle: {
            color: rgbaColor(getThemeColor('gray-700'))
          }
        }
      },
      {
        indicator: [
          { text: 'Exterior', max: 100 },
          { text: 'Take pictures', max: 100 },
          { text: 'system', max: 100 },
          { text: 'performance', max: 100 },
          { text: 'screen', max: 100 }
        ],
        radius: 85,
        center: getCenter()[1],
        splitLine: {
          lineStyle: {
            color: rgbaColor(getThemeColor('gray-700'))
          }
        }
      },
      {
        indicator: months.map(month => ({
          text: month,
          max: 100
        })),
        center: getCenter()[2],
        radius: 85,
        splitLine: {
          lineStyle: {
            color: rgbaColor(getThemeColor('gray-700'))
          }
        }
      }
    ],

    series: [
      {
        type: 'radar',
        tooltip: {
          trigger: 'item'
        },
        areaStyle: {
          color: rgbaColor(getThemeColor()['info'], 0.5)
        },
        data: [
          {
            value: [60, 73, 85, 40],
            name: 'A software',
            itemStyle: {
              color: getThemeColor('info')
            }
          }
        ]
      },
      {
        type: 'radar',
        radarIndex: 1,
        data: [
          {
            value: [85, 90, 90, 95, 95],
            name: 'A staple mobile phone',
            itemStyle: {
              color: rgbaColor(getThemeColor('primary'), 0.8)
            },
            areaStyle: {
              color: rgbaColor(getThemeColor('primary'), 0.3)
            }
          },
          {
            value: [95, 80, 75, 90, 93],
            name: 'A fruit phone',
            itemStyle: {
              color: getThemeColor('success')
            },
            areaStyle: {
              color: rgbaColor(getThemeColor('success'), 0.3)
            }
          }
        ]
      },
      {
        type: 'radar',
        radarIndex: 2,
        areaStyle: {},
        tooltip: {
          show: false
        },
        data: [
          {
            name: 'Precipitation',
            value: [2.6, 5.9, 9.0, 26.4, 28.7, 70.7, 75.6, 82.2, 48.7, 18.8, 6.0, 2.3],
            itemStyle: {
              color: getThemeColor('primary')
            },
            areaStyle: {
              color: rgbaColor(getThemeColor('primary'), 0.5)
            }
          },
          {
            name: 'Evaporation',
            value: [2.0, 4.9, 7.0, 23.2, 25.6, 76.7, 35.6, 62.2, 32.6, 20.0, 6.4, 3.3],
            itemStyle: {
              color: getThemeColor('warning')
            },
            areaStyle: {
              color: rgbaColor(getThemeColor('warning'), 0.5)
            }
          }
        ]
      }
    ]
  });

    //------- Responsive on window resize -------
    
    const updateDimensions = () => {
      chartRef.current.getEchartsInstance().setOption({
        radar: getCenter().map(item => ({
          center: item
        }))
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
        style={{
          height: breakpoints.up('lg')
            ? breakpoints.up('xxl')
              ? '21rem'
              : '32rem'
            : '50rem'
        }}
      />
    );
  }
`;

const RadarMultipleChart = () => {
  return (
    <OrkestraComponentCard dir="ltr">
      <OrkestraComponentCard.Header title="Radar multiple chart" light={false} />
      <OrkestraComponentCard.Body
        code={chartCode}
        language="jsx"
        scope={{
          ReactEchart,
          echarts,
          useAppContext,
          rgbaColor,
          useBreakpoints,
          tooltipFormatter
        }}
      />
    </OrkestraComponentCard>
  );
};

export default RadarMultipleChart;
