import React from 'react';
import FalconComponentCard from 'components/common/FalconComponentCard';

import { BarChart } from 'echarts/charts';
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
  BarChart,
  CanvasRenderer,
  LegendComponent
]);

const chartCode = `function ChartOptions() {
  const { getThemeColor } = useAppContext();
  let data = Array.from(Array(7).keys()).map(() => Math.round(Math.random() * 200));
  const chartRef = useRef(null);

  const run = () => {
    data = data.map(item =>
      Math.random() > 0.9
        ? item + Math.round(Math.random() * 2000)
        : item + Math.round(Math.random() * 200)
    );

    if (chartRef.current?.getEchartsInstance) {
      chartRef.current.getEchartsInstance().setOption({
        series: [
          {
            data
          }
        ]
      });
    }
  };

  useEffect(() => {
    const initialTimeout = setTimeout(() => {
      chartRef.current && run();
    }, 100);

    const interval = setInterval(() => {
      chartRef.current && run();
    }, 5000); // Reduced frequency from 3s to 5s

    return () => {
      clearTimeout(initialTimeout);
      clearInterval(interval);
    };
  }, []);

  const getOption = () => ({
    xAxis: {
      max: 'dataMax',
      splitLine: {
        lineStyle: {
          color: getThemeColor('gray-200')
        }
      },
      axisLabel: {
        color: getThemeColor('gray-500')
      }
    },
    yAxis: {
      type: 'category',
      data: ['A', 'B', 'C', 'D', 'E', 'F', 'G'],
      inverse: true,
      axisLabel: {
        color: getThemeColor('gray-500')
      },
      axisLine: {
        show: true,
        lineStyle: {
          color: getThemeColor('gray-300')
        }
      },
      axisTick: {
        show: false
      },
      animationDuration: 300,
      animationDurationUpdate: 300,
      max: 4 // only the largest 5 bars will be displayed
    },
    series: [
      {
        realtimeSort: true,
        name: 'X',
        type: 'bar',
        data: data,
        label: {
          show: true,
          position: 'right',
          color: getThemeColor('gray-700'),
          fontWeight: 500,
          valueAnimation: true
        },
        itemStyle: {
          color: getThemeColor('primary'),
          borderRadius: [0, 3, 3, 0]
        }
      }
    ],
    animationDuration: 0,
    animationDurationUpdate: 3000,
    animationEasing: 'linear',
    animationEasingUpdate: 'linear',
    grid: {
      right: '10%',
      left: 5,
      bottom: 5,
      top: 5,
      containLabel: true
    }
    });
    
    return (
      <ReactEchart
        echarts={echarts}
        ref={chartRef}
        option={getOption()}
        style={{ height: '21.88rem' }}
      />
    );
  }
`;

const RaceChart = () => {
  return (
    <FalconComponentCard className="h-100">
      <FalconComponentCard.Header title="Bar race chart" light={false} />
      <FalconComponentCard.Body
        code={chartCode}
        language="jsx"
        scope={{
          ReactEchart,
          echarts,
          useAppContext,
          useRef: React.useRef,
          useEffect: React.useEffect
        }}
      />
    </FalconComponentCard>
  );
};

export default RaceChart;
