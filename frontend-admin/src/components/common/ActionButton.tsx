
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome';
import { Button, OverlayTrigger, Tooltip, ButtonProps } from 'react-bootstrap';
import { IconProp } from '@fortawesome/fontawesome-svg-core';
import { OverlayTriggerProps } from 'react-bootstrap/OverlayTrigger';

interface ActionButtonProps extends Omit<ButtonProps, 'title'> {
  placement?: OverlayTriggerProps['placement'];
  title: string;
  icon: IconProp;
}

const ActionButton: React.FC<ActionButtonProps> = ({ 
  placement = 'top', 
  title, 
  icon, 
  ...rest 
}) => {
  return (
    <OverlayTrigger
      placement={placement}
      overlay={<Tooltip style={{ position: 'fixed' }}>{title}</Tooltip>}
    >
      <Button {...rest}>
        <FontAwesomeIcon icon={icon} />
      </Button>
    </OverlayTrigger>
  );
};

export default ActionButton;
