import { useState, useEffect, useRef, MouseEvent } from 'react';
import { Card, Col, Form, Row, Table, Button } from 'react-bootstrap';
import Flex from 'components/common/Flex';
import SubtleBadge from 'components/common/SubtleBadge';
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome';
import classNames from 'classnames';
import GrossRevenueChart from './GrossRevenueChart';
import OrkestraLink from 'components/common/OrkestraLink';
import { IconProp } from '@fortawesome/fontawesome-svg-core';

const months = [
  'Jan',
  'Feb',
  'Mar',
  'Apr',
  'May',
  'Jun',
  'Jul',
  'Aug',
  'Sep',
  'Oct',
  'Nov',
  'Dec'
];

const grossTableRows = [
  {
    id: 1,
    title: 'Point of sale',
    revenue: '$791.64',
    marketValue: {
      up: false,
      value: '13%'
    }
  },
  {
    id: 2,
    title: 'Online Store',
    revenue: '$113.86',
    marketValue: {
      up: true,
      value: '178%'
    }
  },
  {
    id: 3,
    title: 'Online Store',
    revenue: '$0.00',
    marketValue: {
      up: false,
      value: ''
    }
  }
];

interface GrossRevenueData {
  [key: string]: number[];
}

interface GrossRevenueProps {
  data: GrossRevenueData;
}

interface ChartRef {
  getEchartsInstance: () => {
    dispatchAction: (action: { type: string; name: string }) => void;
  };
}

const GrossRevenue = ({ data }: GrossRevenueProps) => {
  const [selectedMonth, setSelectedMonth] = useState(months[0]);
  const [previousMonth, setPreviousMonth] = useState(months[11]);
  const chartRef = useRef(null);

  useEffect(() => {
    if (selectedMonth) {
      const monthIndex = months.indexOf(selectedMonth) - 1;
      const prevMon = monthIndex >= 0 ? months[monthIndex] : months[11];

      setPreviousMonth(prevMon);
    }
  }, [selectedMonth]);

  const handleLegend = (event: MouseEvent<HTMLButtonElement>, name: string) => {
    if (chartRef.current) {
      (chartRef.current as ChartRef).getEchartsInstance().dispatchAction({
        type: 'legendToggleSelect',
        name: name
      });
    }
    (event.target as HTMLElement)
      .closest('button')
      ?.classList.toggle('opacity-50');
  };

  return (
    <Card className="h-100">
      <Card.Header>
        <Row className="justify-content-between gx-0">
          <Col xs="auto">
            <h1 className="fs-9 text-900">Gross revenue</h1>
            <Flex>
              <h4 className="text-primary mb-0">$165.50</h4>
              <div className="ms-3">
                <SubtleBadge pill bg="primary">
                  <FontAwesomeIcon icon="caret-up" /> 5%
                </SubtleBadge>
              </div>
            </Flex>
          </Col>
          <Col xs="auto">
            <Form.Select
              size="sm"
              className="pe-4"
              onChange={({ target }) => setSelectedMonth(target.value)}
            >
              {months.map(mon => (
                <option key={mon} value={mon}>
                  {mon}
                </option>
              ))}
            </Form.Select>
          </Col>
        </Row>
      </Card.Header>
      <Card.Body className="pt-0 pb-3 h-100">
        <div className="mx-ncard">
          <Table borderless className="font-sans-serif fw-medium fs-10">
            <tbody>
              {grossTableRows.map(row => (
                <tr key={row.id}>
                  <td className="pb-2 pt-0">{row.title}</td>
                  <td className="pb-2 pt-0 text-end" style={{ width: '20%' }}>
                    {row.revenue}
                  </td>
                  <td
                    className="pb-2 pt-0 text-end text-700"
                    style={{ maxWidth: '20%' }}
                  >
                    {row.marketValue.value && (
                      <FontAwesomeIcon
                        icon={
                          (row.marketValue.up
                            ? 'long-arrow-alt-up'
                            : 'long-arrow-alt-down') as IconProp
                        }
                        className={classNames('me-1', {
                          'text-success': row.marketValue.up,
                          'text-danger': !row.marketValue.up
                        })}
                      />
                    )}

                    {row.marketValue.value ? row.marketValue.value : '-'}
                  </td>
                </tr>
              ))}
            </tbody>
          </Table>
          <GrossRevenueChart
            ref={chartRef}
            selectedMonth={selectedMonth}
            previousMonth={previousMonth}
            data={data}
            className="px-3 h-100"
            style={{ minHeight: '14.375rem' }}
          />
        </div>
      </Card.Body>
      <Card.Footer as={Flex} className="border-top py-2 flex-between-center">
        <Flex>
          <Button
            variant="text"
            size="sm"
            className="d-flex align-items-center p-0 shadow-none"
            onClick={event => handleLegend(event, 'currentMonth')}
          >
            <FontAwesomeIcon
              icon="circle"
              className="text-primary fs-11 me-1"
            />
            <span className="text">{selectedMonth}</span>
          </Button>
          <Button
            variant="text"
            size="sm"
            className="d-flex align-items-center p-0 shadow-none ms-2"
            onClick={event => handleLegend(event, 'prevMonth')}
          >
            <FontAwesomeIcon icon="circle" className="text-300 fs-11 me-1" />
            <span className="text">{previousMonth}</span>
          </Button>
        </Flex>
        <OrkestraLink title="View report" className="px-0" />
      </Card.Footer>
    </Card>
  );
};

export default GrossRevenue;
