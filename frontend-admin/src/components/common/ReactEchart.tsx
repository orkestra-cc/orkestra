import { useEffect, useRef, forwardRef } from 'react';
import ReactEChartsCore from 'echarts-for-react/lib/core';
import type { EChartsReactProps } from 'echarts-for-react';
import { useAppContext } from 'providers/AppProvider';

type ReactEchartProps = EChartsReactProps;

const ReactEchart = forwardRef<ReactEChartsCore, ReactEchartProps>((props, ref) => {
  const internalRef = useRef<ReactEChartsCore>(null);
  const chartRef = ref || internalRef;
  const {
    config: { isFluid, isNavbarVerticalCollapsed }
  } = useAppContext();

  useEffect(() => {
    const chartInstance = typeof chartRef === 'function' ? null : chartRef?.current;
    if (chartInstance) {
      chartInstance.getEchartsInstance()?.resize();
    }
  }, [isFluid, isNavbarVerticalCollapsed, chartRef]);

  // echarts prop is required for ReactEChartsCore to work properly
  if (!props.echarts) {
    console.error('ReactEchart: echarts prop is required');
    return null;
  }

  return <ReactEChartsCore ref={chartRef} {...props} />;
});

ReactEchart.displayName = 'ReactEchart';

export default ReactEchart;
