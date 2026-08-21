import { ReactNode } from 'react';
import classNames from 'classnames';
import { Card, Spinner } from 'react-bootstrap';
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome';
import { IconProp } from '@fortawesome/fontawesome-svg-core';
import { BadgeColor } from 'components/common/SubtleBadge';

// StatCard is the Orkestra ERP-style KPI tile and the canonical summary-row
// card for admin dashboards: a 4px left accent edge, a faded 2x icon,
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
  /** ReactNode, not just number|string, so a call site can animate the figure
   *  (CountUp) or unit-annotate it without reimplementing the tile. */
  value: ReactNode;
  icon: IconProp;
  color: BadgeColor;
  /** Status hue for the accent edge — pass only when the state earns it. */
  accent?: BadgeColor;
  subtitle?: ReactNode;
  /** Drill-down slot under the value — normally a Link to the page this
   *  metric counts. Its absence is what used to force a dashboard whose
   *  tiles all drill down into a bespoke copy of this component. */
  footer?: ReactNode;
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
  footer,
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
    {/* Two full-height columns, each pinning its own bottom element. The icon
        anchors to the card, never to the text above it: a row mixing tiles
        that drill down with tiles that don't — a counter whose rows no list
        can show has nowhere to send the operator — otherwise scatters its
        icons across three heights. */}
    <Card.Body className="d-flex justify-content-between">
      <div className="d-flex flex-column">
        {/* .h6/.h3 utility classes, not heading tags: a KPI tile's title
            and value are data, and real headings here skipped levels in
            every page outline they appeared in. */}
        <div className="h6 text-muted mb-1 pe-4">{title}</div>
        <div className="h3 mb-0 fw-bold text-900">
          {loading ? <Spinner animation="border" size="sm" /> : value}
        </div>
        {subtitle && <small className="text-muted">{subtitle}</small>}
        {/* mt-auto so a row of tiles shares one link line rather than a
            ragged one. It sits in the text column, not across the card: the
            icon column is 32px, so there is no width worth reclaiming. */}
        {footer && <div className="mt-auto pt-3">{footer}</div>}
      </div>
      {/* 2x at half opacity, not 3x at 75%: this is a category marker, and
          at 48px in a saturated status hue it was the loudest thing in an
          Operate tile — louder than the datum it labels. The ribbon owns
          the top-right corner, so the icon stays bottom-right. */}
      <div className={`text-${color} align-self-end ms-2`}>
        <FontAwesomeIcon icon={icon} size="2x" className="opacity-50" />
      </div>
    </Card.Body>
  </Card>
);

export default StatCard;
