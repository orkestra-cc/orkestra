
import { Col, Row } from 'react-bootstrap';
import StatisticsCard from './StatisticsCard';
import { statsData } from 'data/dashboard/saas';
import { BadgeColor } from 'components/common/SubtleBadge';

interface StatData {
  title: string;
  value: number;
  decimal?: boolean;
  suffix?: string;
  prefix?: string;
  valueClassName?: string;
  linkText: string;
  link: string;
  badgeText?: string;
  badgeBg?: BadgeColor;
  image?: string;
  className?: string;
}

const StatisticsCards = () => {
  return (
    <Row className="g-3 mb-3">
      {(statsData as StatData[]).map((stat: StatData, index: number) => (
        <Col key={stat.title} sm={index === 2 ? 12 : 6} md={4}>
          <StatisticsCard stat={stat} style={{ minWidth: '12rem' }} />
        </Col>
      ))}
    </Row>
  );
};

export default StatisticsCards;
