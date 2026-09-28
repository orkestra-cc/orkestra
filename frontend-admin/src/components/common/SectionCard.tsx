import { ReactNode } from 'react';
import { Card } from 'react-bootstrap';
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome';
import { IconProp } from '@fortawesome/fontawesome-svg-core';

// SectionCard is the carded shell for a titled content panel: a tinted header
// with an icon + title, an optional subtitle under it and an optional
// right-aligned actions slot, over a plain body. It pairs with StatCard — StatCard for the KPI summary row,
// SectionCard for the panels beneath it.
export interface SectionCardProps {
  icon: IconProp;
  iconColor?: string;
  title: string;
  /** Muted line(s) under the title — e.g. what the panel is about. */
  subtitle?: ReactNode;
  headerEnd?: ReactNode;
  /** Classes for the body — e.g. `p-0` for a table that runs edge to edge. */
  bodyClassName?: string;
  children: ReactNode;
}

const SectionCard = ({
  icon,
  iconColor = 'primary',
  title,
  subtitle,
  headerEnd,
  bodyClassName,
  children
}: SectionCardProps) => (
  <Card>
    <Card.Header className="bg-body-tertiary d-flex align-items-center justify-content-between">
      <div className="min-w-0">
        <h5 className="mb-0">
          <FontAwesomeIcon icon={icon} className={`me-2 text-${iconColor}`} />
          {title}
        </h5>
        {subtitle && <div className="fs-10 text-600 mt-1">{subtitle}</div>}
      </div>
      {headerEnd}
    </Card.Header>
    <Card.Body className={bodyClassName}>{children}</Card.Body>
  </Card>
);

export default SectionCard;
