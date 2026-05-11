import { useState } from 'react';
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome';
import { OverlayTrigger, Tooltip } from 'react-bootstrap';
import { IconProp } from '@fortawesome/fontawesome-svg-core';

const ChatMessageOptions = () => {
  const [actions] = useState([
    {
      tooltip: 'Forward',
      icon: 'share' as IconProp
    },
    {
      tooltip: 'Archive',
      icon: 'archive' as IconProp
    },
    {
      tooltip: 'Edit',
      icon: 'edit' as IconProp
    },
    {
      tooltip: 'Remove',
      icon: 'trash-alt' as IconProp
    }
  ]);

  return (
    <ul className="hover-actions position-relative list-inline mb-0 text-400 mx-2">
      {actions.map(action => (
        <li
          key={action.tooltip}
          className="list-inline-item cursor-pointer chat-option-hover"
        >
          <OverlayTrigger
            overlay={
              <Tooltip style={{ position: 'fixed' }}>{action.tooltip}</Tooltip>
            }
          >
            <div>
              <FontAwesomeIcon icon={action.icon} className="d-inline-block" />
            </div>
          </OverlayTrigger>
        </li>
      ))}
    </ul>
  );
};

export default ChatMessageOptions;
