
import { Card, Col, Form, Image, Row, Table } from 'react-bootstrap';
import FalconCardHeader from 'components/common/FalconCardHeader';
import CardDropdown from 'components/common/CardDropdown';
import classNames from 'classnames';
import Flex from 'components/common/Flex';
import { Link } from 'react-router';
import SubtleBadge, { BadgeColor } from 'components/common/SubtleBadge';
import SimpleBar from 'simplebar-react';
import FalconLink from 'components/common/FalconLink';

interface TransactionSummaryData {
  id: string | number;
  img: string;
  title: string;
  subtitle: string;
  status: 'Completed' | 'Pending' | 'Rejected';
  amount: string;
  date: string;
}

interface TransactionItemProps {
  summary: TransactionSummaryData;
  isLast: boolean;
}

interface TransactionSummaryProps {
  data: TransactionSummaryData[];
}

const getBadgeColor = (status: string): BadgeColor => {
  switch (status) {
    case 'Completed':
      return 'success';
    case 'Pending':
      return 'warning';
    case 'Rejected':
      return 'danger';
    default:
      return 'secondary';
  }
};

const TransactionItem = ({
  summary: { img, title, subtitle, status, amount, date },
  isLast
}: TransactionItemProps) => {
  return (
    <tr className={classNames({ 'border-0': isLast })}>
      <td
        className={classNames('align-middle ps-0 text-nowrap', {
          'border-0': isLast
        })}
      >
        <Flex alignItems="center" className="position-relative">
          <Image src={img} alt={title} className="me-2" width={30} />
          <div className="flex-1">
            <Link to="#!" className="stretched-link">
              <h6 className="mb-0">{title}</h6>
            </Link>
            <p className="mb-0">{subtitle}</p>
          </div>
        </Flex>
      </td>
      <td
        className={classNames('align-middle px-4', { 'border-0': isLast })}
        style={{ width: '1%' }}
      >
        <SubtleBadge
          bg={getBadgeColor(status)}
          className="fs-10 w-100"
        >
          {status}
        </SubtleBadge>
      </td>
      <td
        className={classNames('align-middle px-4 text-end text-nowrap', {
          'border-0': isLast
        })}
        style={{ width: '1%' }}
      >
        <h6 className="mb-0">{amount}</h6>
        <p className="fs-11 mb-0">{date}</p>
      </td>
      <td
        className={classNames('align-middle ps-4 pe-1', { 'border-0': isLast })}
        style={{ width: '130px', minWidth: '130px' }}
      >
        <Form.Select size="sm" className="px-2">
          <option value="action">Action</option>
          <option value="archive">Archive</option>
          <option value="delete">Delete</option>
        </Form.Select>
      </td>
    </tr>
  );
};

const TransactionSummary = ({ data: transactions }: TransactionSummaryProps) => {
  return (
    <Card className="overflow-hidden">
      <FalconCardHeader
        title="Transaction Summary"
        titleTag="h6"
        className="py-2"
        light
        endEl={<CardDropdown />}
      />
      <Card.Body className="py-0">
        <SimpleBar>
          <Table className="table-dashboard mb-0 fs-10">
            <tbody>
              {transactions.map((item, index) => (
                <TransactionItem
                  key={item.id}
                  isLast={index === transactions.length - 1}
                  summary={item}
                />
              ))}
            </tbody>
          </Table>
        </SimpleBar>
      </Card.Body>
      <Card.Footer className="bg-body-tertiary py-2">
        <Row className="flex-between-center">
          <Col xs="auto">
            <Form.Select size="sm">
              <option value="last 7 days">Last 7 days</option>
              <option value="last month">Last Month</option>
              <option value="last year">Last Year</option>
            </Form.Select>
          </Col>
          <Col xs="auto">
            <FalconLink title="View All" className="px-0 fw-medium" />
          </Col>
        </Row>
      </Card.Footer>
    </Card>
  );
};

export default TransactionSummary;
