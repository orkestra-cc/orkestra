import CardDropdown from 'components/common/CardDropdown';
import FalconCardHeader from 'components/common/FalconCardHeader';
import Flex from 'components/common/Flex';
import { LineChart } from 'echarts/charts';
import * as echarts from 'echarts/core';
import { useState } from 'react';
import { Card, Form } from 'react-bootstrap';

import { months } from 'data/common';
import {
  GridComponent,
  LegendComponent,
  TitleComponent,
  TooltipComponent
} from 'echarts/components';
import { CanvasRenderer } from 'echarts/renderers';
import { capitalize, rgbaColor } from 'helpers/utils';
import { useAppContext } from 'providers/AppProvider';
import ReactEchart from 'components/common/ReactEchart';

// TypeScript interfaces
interface TotalSalesProps {
  data: { [key: number]: number[] };
}

echarts.use([
  TitleComponent,
  TooltipComponent,
  GridComponent,
  LineChart,
  CanvasRenderer,
  LegendComponent
]);

const getOptions = (getThemeColor: (color: string) => string, month: string, data: number[]) => ({
  color: getThemeColor('gray-100'),
  tooltip: {
    trigger: 'axis',
    padding: [7, 10],
    backgroundColor: getThemeColor('gray-100'),
    borderColor: getThemeColor('gray-100'),
    textStyle: { color: getThemeColor('gray-1100') },
    borderWidth: 1,
    formatter: (params: { name: string; value: number }[]) => {
      const { name, value } = params[0];
      return `${month} ${name} : ${value}`;
    },
    transitionDuration: 0
  },
  xAxis: {
    type: 'category',
    data: [1, 3, 5, 7, 9, 11, 13, 15, 17, 19, 21, 23],
    boundaryGap: false,
    axisPointer: {
      lineStyle: {
        color: getThemeColor('gray-300'),
        type: 'dashed'
      }
    },
    splitLine: { show: false },
    axisLine: {
      lineStyle: {
        color: getThemeColor('gray-300'),
        type: 'dashed'
      }
    },
    axisTick: { show: false },
    axisLabel: {
      color: getThemeColor('gray-400'),
      formatter: (value: string | number) => `${capitalize(month.slice(0, 3))} ${value}`,
      margin: 15
    }
  },
  yAxis: {
    type: 'value',
    axisPointer: { show: false },
    splitLine: {
      lineStyle: {
        color: getThemeColor('gray-300'),
        type: 'dashed'
      }
    },
    boundaryGap: false,
    axisLabel: {
      show: true,
      color: getThemeColor('gray-400'),
      margin: 15
    },
    axisTick: { show: false },
    axisLine: { show: false }
  },
  series: [
    {
      type: 'line',
      data,
      lineStyle: { color: getThemeColor('primary') },
      itemStyle: {
        borderColor: getThemeColor('primary'),
        borderWidth: 2
      },
      symbol: 'circle',
      symbolSize: 10,
      smooth: false,
      areaStyle: {
        color: {
          type: 'linear',
          x: 0,
          y: 0,
          x2: 0,
          y2: 1,
          colorStops: [
            {
              offset: 0,
              color: rgbaColor(getThemeColor('primary'), 0.2)
            },
            {
              offset: 1,
              color: rgbaColor(getThemeColor('primary'), 0)
            }
          ]
        }
      }
    }
  ],
  grid: { right: 10, left: 0, bottom: 0, top: 10, containLabel: true }
});

const TotalSales = ({ data }: TotalSalesProps) => {
  const [month, setMonth] = useState<number>(0);
  const { getThemeColor } = useAppContext();
  return (
    <Card className="h-100">
      <FalconCardHeader
        title="Total Sales"
        titleTag="h6"
        className="pb-0"
        endEl={
          <Flex>
            <Form.Select
              size="sm"
              value={month}
              onChange={e => setMonth(Number(e.target.value))}
              className="me-2"
            >
              {months.map((monthName: string, index: number) => (
                <option value={index} key={monthName}>
                  {monthName}
                </option>
              ))}
            </Form.Select>
            <CardDropdown />
          </Flex>
        }
      />

      <Card.Body>
        <ReactEchart
          echarts={echarts}
          option={getOptions(getThemeColor, months[month], data[month])}
          style={{ height: '18.4375rem' }}
        />
      </Card.Body>
    </Card>
  );
};

export default TotalSales;
