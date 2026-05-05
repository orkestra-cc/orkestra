
import { Card } from 'react-bootstrap';
import corner1 from 'assets/img/illustrations/corner-1.png';
import { Link } from 'react-router';
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome';
import Background from 'components/common/Background';

interface SpaceWarningProps {
  className?: string;
}

const SpaceWarning = ({ className }: SpaceWarningProps) => (
  <Card className={`overflow-hidden ${className}`}>
    <Background image={corner1} className="p-x1 bg-card" />
    <Card.Body className="position-relative">
      <h5 className="text-warning">Running out of your space?</h5>
      <p className="fs-10 mb-0">
        Your storage will be running out soon. Get more
        <br /> space and powerful productivity features.
      </p>
      <Link
        to="#!"
        className="btn btn-link fs-10 text-warning mt-lg-3 ps-0"
      >
        Upgrade storage
        <FontAwesomeIcon icon="chevron-right" className="ms-1 fs-11" />
      </Link>
    </Card.Body>
  </Card>
);

export default SpaceWarning;
