
import { Card, Col, Row } from 'react-bootstrap';
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome';
import Flex from 'components/common/Flex';
import { PieChart } from 'echarts/charts';
import * as echarts from 'echarts/core';
import BasicECharts from 'components/common/BasicEChart';
import {
  GridComponent,
  TitleComponent,
  TooltipComponent
} from 'echarts/components';
import { CanvasRenderer } from 'echarts/renderers';
import { useAppContext } from 'providers/AppProvider';

// TypeScript interfaces
interface MarketShareData {
  name: string;
  value: number;
  color: string;
}

interface MarketShareProps {
  data: MarketShareData[];
  radius?: string | string[];
}

interface MarketShareItemProps {
  item: MarketShareData;
  index: number;
  total: number;
}

echarts.use([
  TitleComponent,
  TooltipComponent,
  GridComponent,
  PieChart,
  CanvasRenderer
]);

const getOptions = (getThemeColor: (color: string) => string, data: MarketShareData[], radius?: string | string[]) => ({
  color: [
    getThemeColor(data[0].color),
    getThemeColor(data[1].color),
    getThemeColor(data[2].color)
  ],

  tooltip: {
    padding: [7, 10],
    transitionDuration: 0,
    backgroundColor: getThemeColor('gray-100'),
    borderColor: getThemeColor('gray-300'),
    textStyle: { color: getThemeColor('gray-1100') },
    borderWidth: 1,
    formatter: (params: { data: { name: string }; percent: number }) =>
      `<strong>${params.data.name}:</strong> ${params.percent}%`
  },
  series: [
    {
      name: '26M',
      type: 'pie',
      radius,
      avoidLabelOverlap: false,
      emphasis: {
        scale: false
      },
      itemStyle: {
        borderWidth: 2,
        borderColor: getThemeColor('gray-100')
      },
      label: {
        show: true,
        position: 'center',
        formatter: '{a}',
        fontSize: 23,
        color: getThemeColor('gray-1100')
      },
      data
    }
  ]
});

const MarketShareItem: React.FC<MarketShareItemProps> = ({ item, index, total }) => {
  const { name, color, value } = item;

  const percentage = ((value * 100) / total).toFixed(0);
  return (
    <Flex
      alignItems="center"
      justifyContent="between"
      className={`fw-semibold fs-11 ${index == 0 && 'mt-3'}`}
    >
      <p className="mb-1">
        <FontAwesomeIcon
          icon="circle"
          className={`me-2 text-${color.split('-')[1] || color || 'text-info'}`}
        />
        {name}
      </p>
      <div className="d-xxl-none">{percentage}%</div>
    </Flex>
  );
};

const MarketShare: React.FC<MarketShareProps> = ({ data, radius }) => {
  const { getThemeColor } = useAppContext();
  const total = data.reduce((acc, val) => val.value + acc, 0);
  return (
    <Card className="h-md-100">
      <Card.Body>
        <Row className="justify-content-between g-0">
          <Col xs={5} sm={6} xxl className="pe-2">
            <h6 className="mt-1">Market Share</h6>

            {data.map((item, index) => (
              <MarketShareItem
                item={item}
                index={index}
                key={index}
                total={total}
              />
            ))}
          </Col>
          <Col xs="auto">
            <div className="ps-0">
              <BasicECharts
                echarts={echarts}
                options={getOptions(getThemeColor, data, radius)}
                style={{ width: '6.625rem', height: '6.625rem' }}
              />
            </div>
          </Col>
        </Row>
      </Card.Body>
    </Card>
  );
};

export default MarketShare;
