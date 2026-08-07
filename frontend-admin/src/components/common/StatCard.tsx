import { ReactNode } from 'react';
import classNames from 'classnames';
import { Card, Spinner } from 'react-bootstrap';
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome';
import { IconProp } from '@fortawesome/fontawesome-svg-core';
import { BadgeColor } from 'components/common/SubtleBadge';

// StatCard is the Orkestra ERP-style KPI tile and the canonical summary-row
// card for admin dashboards: a 4px left accent edge, a large faded 3x icon,
// the headline value, an optional subtitle, and — when the metric needs
// attention — a diagonal corner ribbon (styled by `.stat-ribbon` in
// theme/_stat-card.scss). Prefer it over bespoke per-page stat cards so every
// dashboard's KPI row looks the same.
//
// Status colors mean status: the accent edge is NEUTRAL at rest and takes a
// hue only through the `accent` prop, which a call site passes when the
// metric's current state earns it (a non-zero pending counter, a degraded
// health check) — never as category identity. `color` styles only the faded
// icon (and is the ribbon's fallback hue).
export interface StatCardProps {
  title: string;
  value: number | string;
  icon: IconProp;
  color: BadgeColor;
  /** Status hue for the accent edge — pass only when the state earns it. */
  accent?: BadgeColor;
  subtitle?: ReactNode;
  badge?: { text: string; bg?: BadgeColor };
  loading?: boolean;
}

const StatCard = ({
  title,
  value,
  icon,
  color,
  accent,
  subtitle,
  badge,
  loading
}: StatCardProps) => (
  <Card
    className={classNames(
      'h-100 position-relative overflow-hidden stat-card',
      accent && `stat-card-accent-${accent}`
    )}
  >
    {badge && (
      <div className={`stat-ribbon stat-ribbon-${badge.bg ?? color}`}>
        <span>{badge.text}</span>
      </div>
    )}
    <Card.Body>
      <div className="d-flex align-items-end justify-content-between">
        <div>
          {/* .h6/.h3 utility classes, not heading tags: a KPI tile's title
              and value are data, and real headings here skipped levels in
              every page outline they appeared in. */}
          <div className="h6 text-muted mb-1 pe-4">{title}</div>
          <div className="h3 mb-0 fw-bold text-900">
            {loading ? <Spinner animation="border" size="sm" /> : value}
          </div>
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
