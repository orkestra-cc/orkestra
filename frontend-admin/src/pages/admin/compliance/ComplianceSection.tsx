import { ReactNode } from 'react';
import { Card } from 'react-bootstrap';
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome';
import { IconProp } from '@fortawesome/fontawesome-svg-core';

// ComplianceSection is the carded shell each tab renders into: a tinted header
// with an icon + title (and an optional right-aligned slot for actions), mirroring
// the ERP GDPR panel's section styling.
interface ComplianceSectionProps {
  icon: IconProp;
  iconColor?: string;
  title: string;
  headerEnd?: ReactNode;
  children: ReactNode;
}

const ComplianceSection = ({
  icon,
  iconColor = 'primary',
  title,
  headerEnd,
  children
}: ComplianceSectionProps) => (
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

export default ComplianceSection;
