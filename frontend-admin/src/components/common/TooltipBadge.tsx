import { OverlayTrigger, Tooltip, OverlayTriggerProps } from 'react-bootstrap';
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome';
import { IconProp } from '@fortawesome/fontawesome-svg-core';

interface TooltipBadgeProps {
  placement?: OverlayTriggerProps['placement'];
  tooltip: string;
  icon: IconProp;
  color?: string;
}

const TooltipBadge = ({
  placement = 'top',
  tooltip,
  icon,
  color = 'primary'
}: TooltipBadgeProps) => {
  return (
    <OverlayTrigger
      placement={placement}
      overlay={<Tooltip style={{ position: 'fixed' }}>{tooltip}</Tooltip>}
    >
      <span>
        <FontAwesomeIcon
          icon={icon}
          transform="shrink-2"
          className={`text-${color} ms-1`}
        />
      </span>
    </OverlayTrigger>
  );
};

export default TooltipBadge;
