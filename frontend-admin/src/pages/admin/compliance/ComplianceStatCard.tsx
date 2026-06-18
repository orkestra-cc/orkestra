import { Badge, Card, Spinner } from 'react-bootstrap';
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome';
import { IconProp } from '@fortawesome/fontawesome-svg-core';
import { BadgeColor } from 'components/common/SubtleBadge';

// ComplianceStatCard is the compact KPI tile used in the page summary row
// (pending erasures, active holds, retention candidates, audit volume): a
// color-accented left border, a large faded icon, the headline value, and an
// optional status badge that only shows when the metric needs attention.
interface ComplianceStatCardProps {
  title: string;
  value: number | string;
  icon: IconProp;
  color: BadgeColor;
  subtitle?: string;
  badgeText?: string;
  loading?: boolean;
}

const ComplianceStatCard = ({
  title,
  value,
  icon,
  color,
  subtitle,
  badgeText,
  loading
}: ComplianceStatCardProps) => (
  <Card className={`h-100 border-start border-4 border-${color}`}>
    <Card.Body>
      <div className="d-flex align-items-center justify-content-between">
        <div>
          <h6 className="text-muted mb-1">{title}</h6>
          <h3 className="mb-0 fw-bold text-900">
            {loading ? <Spinner animation="border" size="sm" /> : value}
          </h3>
          {subtitle && <small className="text-muted">{subtitle}</small>}
        </div>
        <div className={`text-${color}`}>
          <FontAwesomeIcon icon={icon} size="3x" className="opacity-75" />
        </div>
      </div>
      {badgeText && (
        <div className="mt-3">
          <Badge bg={color}>{badgeText}</Badge>
        </div>
      )}
    </Card.Body>
  </Card>
);

export default ComplianceStatCard;
