import { useState } from 'react';
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome';
import classNames from 'classnames';
import CardDropdown from 'components/common/CardDropdown';
import OrkestraCardHeader from 'components/common/OrkestraCardHeader';
import Flex from 'components/common/Flex';
import { GaugeChart } from 'echarts/charts';
import {
  GridComponent,
  LegendComponent,
  TitleComponent,
  TooltipComponent
} from 'echarts/components';
import * as echarts from 'echarts/core';
import { CanvasRenderer } from 'echarts/renderers';
import { Card, Col, Form, Row } from 'react-bootstrap';
import { Link } from 'react-router';
import { useAppContext } from 'providers/AppProvider';
import ReactEchart from 'components/common/ReactEchart';

echarts.use([
  TitleComponent,
  TooltipComponent,
  GridComponent,
  GaugeChart,
  CanvasRenderer,
  LegendComponent
]);

type ThemeColorGetter = (color: string) => string;

const getOptions = (getThemeColor: ThemeColorGetter, data: number) => ({
  series: [
    {
      type: 'gauge',
      startAngle: 90,
      endAngle: -270,
      radius: '90%',
      pointer: {
        show: false
      },
      progress: {
        show: true,
        overlap: false,
        roundCap: true,
        clip: false,
        itemStyle: {
          color: getThemeColor('primary')
        }
      },
      axisLine: {
        lineStyle: {
          width: 8,
          color: [[1, getThemeColor('gray-200')]]
        }
      },
      splitLine: {
        show: false
      },
      axisTick: {
        show: false
      },
      axisLabel: {
        show: false
      },
      data: [
        {
          value: data,
          detail: {
            offsetCenter: ['7%', '4%']
          }
        }
      ],
      detail: {
        width: 50,
        height: 14,
        fontSize: 28,
        fontWeight: 500,
        fontFamily: 'poppins',
        color: getThemeColor('gray-500'),
        formatter: '{value}%'
      }
    }
  ]
});

interface BandwidthSavedProps {
  bodyClassName?: string;
}

const BandwidthSaved = ({ bodyClassName }: BandwidthSavedProps) => {
  const { getThemeColor } = useAppContext();
  const [total] = useState(38.44);
  const [saved] = useState(35.75);

  return (
    <Card className="h-100">
      <OrkestraCardHeader
        title="Bandwidth Saved"
        titleTag="h6"
        className="py-2"
        light
        endEl={<CardDropdown />}
      />
      <Card.Body
        className={classNames(bodyClassName, 'h-100')}
        as={Flex}
        direction="column"
        alignItems="center"
      >
        <ReactEchart
          echarts={echarts}
          option={getOptions(getThemeColor, 93)}
          style={{ height: '10.3rem', width: '10.3rem' }}
        />
        <div className="text-center mt-3">
          <h6 className="fs-9 mb-1">
            <FontAwesomeIcon
              icon="check"
              transform="shrink-2"
              className="text-success me-1"
            />
            {saved} GB saved
          </h6>
          <p className="fs-10 mb-0">{total} GB total bandwidth</p>
        </div>
      </Card.Body>
      <Card.Footer className="bg-body-tertiary py-2">
        <Row className="flex-between-center">
          <Col xs="auto">
            <Form.Select size="sm" className="me-2">
              <option>Last 6 Months</option>
              <option>Last Year</option>
              <option>Last 2 Year</option>
            </Form.Select>
          </Col>
          <Col xs="auto">
            <Link className="fs-10 font-sans-serif" to="#!">
              Help
            </Link>
          </Col>
        </Row>
      </Card.Footer>
    </Card>
  );
};

export default BandwidthSaved;
