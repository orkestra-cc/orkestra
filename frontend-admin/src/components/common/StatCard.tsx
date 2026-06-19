import { ReactNode } from 'react';
import { Card, Spinner } from 'react-bootstrap';
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome';
import { IconProp } from '@fortawesome/fontawesome-svg-core';
import { BadgeColor } from 'components/common/SubtleBadge';

// StatCard is the Orkestra ERP-style KPI tile and the canonical summary-row
// card for admin dashboards: a full 4px color-accented border, a large faded
// 3x icon, the headline value, an optional subtitle, and — when the metric
// needs attention — a diagonal corner ribbon (styled by `.stat-ribbon` in
// theme/_stat-card.scss). Prefer it over bespoke per-page stat cards so every
// dashboard's KPI row looks the same.
export interface StatCardProps {
  title: string;
  value: number | string;
  icon: IconProp;
  color: BadgeColor;
  subtitle?: ReactNode;
  badge?: { text: string; bg?: BadgeColor };
  loading?: boolean;
}

const StatCard = ({
  title,
  value,
  icon,
  color,
  subtitle,
  badge,
  loading
}: StatCardProps) => (
  <Card
    className={`h-100 position-relative overflow-hidden border-4 border-${color}`}
  >
    {badge && (
      <div className={`stat-ribbon stat-ribbon-${badge.bg ?? color}`}>
        <span>{badge.text}</span>
      </div>
    )}
    <Card.Body>
      <div className="d-flex align-items-end justify-content-between">
        <div>
          <h6 className="text-muted mb-1 pe-4">{title}</h6>
          <h3 className="mb-0 fw-bold text-900">
            {loading ? <Spinner animation="border" size="sm" /> : value}
          </h3>
          {subtitle && <small className="text-muted">{subtitle}</small>}
        </div>
        <div className={`text-${color} align-self-end`}>
          <FontAwesomeIcon icon={icon} size="3x" className="opacity-75" />
        </div>
      </div>
    </Card.Body>
  </Card>
);

export default StatCard;
