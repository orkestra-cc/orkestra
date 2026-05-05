
import { OverlayTrigger, Tooltip } from 'react-bootstrap';
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome';
import { OverlayTriggerProps } from 'react-bootstrap/OverlayTrigger';

interface VerifiedBadgeProps {
  placement?: OverlayTriggerProps['placement'];
}

const VerifiedBadge: React.FC<VerifiedBadgeProps> = ({ placement = 'top' }) => {
  return (
    <OverlayTrigger
      placement={placement}
      overlay={<Tooltip style={{ position: 'fixed' }}>Verified</Tooltip>}
    >
      <span>
        <FontAwesomeIcon
          icon="check-circle"
          transform="shrink-4 down-2"
          className="text-primary me-1"
        />
      </span>
    </OverlayTrigger>
  );
};

export default VerifiedBadge;
