
import { Col, Row } from 'react-bootstrap';
import { statsData } from 'data/dashboard/saas';
import StatisticsCard from 'components/dashboards/saas/stats-cards/StatisticsCard';
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

const Statistics = () => {
  return (
    <Row className="g-3 mb-3">
      {(statsData.slice(0, 2) as StatData[]).map((stat: StatData) => (
        <Col key={stat.title} sm={6}>
          <StatisticsCard stat={stat} style={{ minWidth: '12rem' }} />
        </Col>
      ))}
    </Row>
  );
};

export default Statistics;
