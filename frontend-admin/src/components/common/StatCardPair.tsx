import { ReactNode } from 'react';
import classNames from 'classnames';
import { Card, Col, Row, Spinner } from 'react-bootstrap';
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome';
import { IconProp } from '@fortawesome/fontawesome-svg-core';
import SubtleBadge, { BadgeColor } from 'components/common/SubtleBadge';

// StatCardPair is StatCard's two-metric sibling: one tile carrying a pair of
// figures an operator reads against each other rather than in isolation —
// issued vs received invoices, sent vs bounced mail, granted vs revoked. Use
// it only for genuine pairs; two unrelated metrics belong in two StatCards,
// where each gets its own accent edge and ribbon.
//
// It shares StatCard's visual language deliberately — the `.stat-card` accent
// edge, `.h6`/`.h3` utility classes rather than heading tags, the same muted
// label register — so the two can sit side by side in one KPI row without
// reading as different components.
//
// The one divergence, and the reason this is a separate component rather than
// a StatCard prop: the corner ribbon is a *card-level* element, and a card
// with two metrics would need two of them overlapping in the same corner. A
// half therefore raises its attention flag as an inline pill. If a metric
// needs the ribbon's weight, it needs its own StatCard.
export interface StatCardHalf {
  title: string;
  /** ReactNode so the call site can animate the figure (CountUp) or annotate
   *  its unit — same contract as StatCard's `value`. */
  value: ReactNode;
  icon: IconProp;
  color: BadgeColor;
  /** Inline pill, not a corner ribbon — see the note above. */
  badge?: { text: string; bg?: BadgeColor };
  /** Drill-down link for this half. Omit when the metric has no page to
   *  open; never point it somewhere the counted rows are not. */
  footer?: ReactNode;
}

export interface StatCardPairProps {
  title: string;
  halves: [StatCardHalf, StatCardHalf];
  /** Status hue for the accent edge, neutral at rest exactly as on StatCard.
   *  With two metrics the caller decides what the pair's combined state is —
   *  the component cannot infer it. */
  accent?: BadgeColor;
  loading?: boolean;
}

const StatCardPair = ({
  title,
  halves,
  accent,
  loading
}: StatCardPairProps) => (
  <Card
    className={classNames(
      'h-100 stat-card',
      accent && `stat-card-accent-${accent}`
    )}
  >
    <Card.Body>
      {/* No large icon in the header: each half carries its own, and a third
          one names nothing while being the tallest thing in the card — which,
          under h-100, sets the height of every other tile in the row. */}
      <div className="h6 text-muted mb-3">{title}</div>
      <Row className="g-0">
        {halves.map((half, index) => (
          <Col
            key={half.title}
            xs={6}
            // border-300, not a bare border-start: the default
            // `--orkestra-border-color` is rgba(white, .05) in dark, which
            // measures ~1.1:1 against the card — no divider at all.
            className={index === 0 ? 'pe-3' : 'ps-3 border-start border-300'}
          >
            <div className="d-flex align-items-center mb-1">
              <FontAwesomeIcon
                icon={half.icon}
                className={`me-2 fs-10 text-${half.color}`}
              />
              <span className="text-muted fs-10">{half.title}</span>
            </div>
            <div className="h3 mb-0 fw-bold text-900">
              {loading ? <Spinner animation="border" size="sm" /> : half.value}
            </div>
            {half.badge && (
              <SubtleBadge
                bg={half.badge.bg ?? half.color}
                pill
                className="mt-1 fs-11"
              >
                {half.badge.text}
              </SubtleBadge>
            )}
            {half.footer && <div className="mt-2">{half.footer}</div>}
          </Col>
        ))}
      </Row>
    </Card.Body>
  </Card>
);

export default StatCardPair;
