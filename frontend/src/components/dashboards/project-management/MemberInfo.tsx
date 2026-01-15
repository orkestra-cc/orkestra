import classNames from 'classnames';
import Avatar from 'components/common/Avatar';
import Flex from 'components/common/Flex';

import { Card, Col, Form, Row, Table } from 'react-bootstrap';
import { Link } from 'react-router';
import SubtleBadge, { BadgeColor } from 'components/common/SubtleBadge';
import BasicECharts from 'components/common/BasicEChart';
import * as echarts from 'echarts/core';
import FalconLink from 'components/common/FalconLink';
import SimpleBar from 'simplebar-react';
import { useAppContext } from 'providers/AppProvider';

interface BadgeInfo {
  color: BadgeColor;
  text?: string;
  amount?: number;
  time?: string;
}

interface MemberData {
  id: string | number;
  name: string;
  img: string;
  role: string;
  status: string;
  attendance: BadgeInfo;
  today: BadgeInfo;
  thisWeek: BadgeInfo;
  data: number[];
}

interface MembersRowProps extends Omit<MemberData, 'id'> {
  isLast: boolean;
}

const MembersRow = ({
  name,
  img,
  role,
  isLast,
  status,
  attendance,
  today,
  thisWeek,
  data
}: MembersRowProps) => {
  const { getThemeColor } = useAppContext();
  return (
    <tr className={classNames({ 'border-bottom border-200': !isLast })}>
      <td>
        <Flex alignItems="center" className="position-relative">
          <Avatar
            className={`status-${status}`}
            size="2xl"
            src={img}
          />
          <div className="flex-1 ms-3">
            <h6 className="mb-0 fw-semibold">
              <Link className="text-1100 stretched-link" to="#!">
                {name}
              </Link>
            </h6>
            <p className="fs-11 mb-0 text-500">{role}</p>
          </div>
        </Flex>
      </td>
      <td className="align-middle text-center fw-semibold">
        <SubtleBadge pill bg={attendance.color}>
          {attendance.text}
        </SubtleBadge>
      </td>
      <td className="align-middle text-center fw-semibold">
        <SubtleBadge pill bg={today.color}>
          {today.amount}%
        </SubtleBadge>
        <p className="fs-11 mb-0">{today.time}</p>
      </td>
      <td className="align-middle">
        <Row className="g-2 justify-content-end">
          <Col xs="auto">
            <SubtleBadge pill bg={thisWeek.color}>
              {thisWeek.amount}%
            </SubtleBadge>
            <p className="fs-11 mb-0">{thisWeek.time}</p>
          </Col>
          <Col xs="auto" className="mt-auto">
            <BasicECharts
              echarts={echarts}
              options={{
                color: getThemeColor('primary'),
                tooltip: { show: false },
                series: [
                  {
                    data
                  }
                ]
              }}
              className="mb-1"
              style={{ width: '3.625rem', height: '1rem' }}
            />
          </Col>
        </Row>
      </td>
    </tr>
  );
};

interface MemberInfoProps {
  data: MemberData[];
}

const MemberInfo = ({ data }: MemberInfoProps) => {
  return (
    <Card className="h-100">
      <Card.Body className="p-0">
        <SimpleBar>
          <Table
            borderless
            className="mb-0 fs-10 border-200 rounded-3 table-dashboard table-member-info"
          >
            <thead className="bg-body-tertiary">
              <tr>
                <th className="text-900">Member info</th>
                <th className="text-900 text-center">Attendance</th>
                <th className="text-900 text-center">Today</th>
                <th className="text-900 text-end">This Week</th>
              </tr>
            </thead>
            <tbody>
              {data.map((info: MemberData, index: number) => (
                <MembersRow
                  {...info}
                  isLast={index === data.length - 1}
                  key={info.id}
                />
              ))}
            </tbody>
          </Table>
        </SimpleBar>
      </Card.Body>

      <Card.Footer className="bg-body-tertiary py-2">
        <Row className="g-0 flex-between-center">
          <Col xs="auto">
            <Form.Select size="sm" className="me-2">
              <option>Last 7 days</option>
              <option>Last Month</option>
              <option>Last Year</option>
            </Form.Select>
          </Col>
          <Col xs="auto">
            <FalconLink title="View All" className="px-0" />
          </Col>
        </Row>
      </Card.Footer>
    </Card>
  );
};

export default MemberInfo;
