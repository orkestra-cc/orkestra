import { ReactNode } from 'react';
import { Card } from 'react-bootstrap';
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome';
import { IconProp } from '@fortawesome/fontawesome-svg-core';

// SectionCard is the carded shell for a titled content panel: a tinted header
// with an icon + title and an optional right-aligned actions slot, over a
// plain body. It pairs with StatCard — StatCard for the KPI summary row,
// SectionCard for the panels beneath it.
export interface SectionCardProps {
  icon: IconProp;
  iconColor?: string;
  title: string;
  headerEnd?: ReactNode;
  children: ReactNode;
}

const SectionCard = ({
  icon,
  iconColor = 'primary',
  title,
  headerEnd,
  children
}: SectionCardProps) => (
  <Card>
    <Card.Header className="bg-body-tertiary d-flex align-items-center justify-content-between">
      <h5 className="mb-0">
        <FontAwesomeIcon icon={icon} className={`me-2 text-${iconColor}`} />
        {title}
      </h5>
      {headerEnd}
    </Card.Header>
    <Card.Body>{children}</Card.Body>
  </Card>
);

export default SectionCard;
