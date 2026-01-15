import Flex from 'components/common/Flex';
import { Card, OverlayTrigger, Tooltip, Dropdown } from 'react-bootstrap';
import IconButton from 'components/common/IconButton';
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome';
import { IconProp } from '@fortawesome/fontawesome-svg-core';
import { useNavigate } from 'react-router';

interface ItemButtonProps {
  tootltip: string;
  icon: IconProp;
  className?: string;
  onClick?: () => void;
}

const ItemButton = ({ tootltip, icon, className, onClick }: ItemButtonProps) => {
  return (
    <OverlayTrigger
      overlay={
        <Tooltip style={{ position: 'fixed' }} id="overlay-trigger-example">
          {tootltip}
        </Tooltip>
      }
    >
      <div className="d-inline-block">
        <IconButton
          variant="falcon-default"
          size="sm"
          icon={icon}
          className={className}
          onClick={onClick}
        />
      </div>
    </OverlayTrigger>
  );
};

const EmailDetailHeader = () => {
  const navigate = useNavigate();

  return (
    <Card className="mb-3">
      <Card.Body as={Flex} justifyContent="between">
        <div>
          <ItemButton
            tootltip="Back to inbox"
            icon={'arrow-left' as IconProp}
            onClick={() => {
              navigate('/email/inbox');
            }}
          />
          <span className="mx-1 mx-sm-2 text-300">|</span>
          <ItemButton tootltip="Archive" icon={'archive' as IconProp} />
          <ItemButton
            tootltip="Delete"
            icon={'trash-alt' as IconProp}
            className="ms-1 ms-sm-2"
          />
          <ItemButton
            tootltip="Mark as unread"
            icon={'envelope' as IconProp}
            className="ms-1 ms-sm-2"
          />
          <ItemButton tootltip="Snooze" icon={'clock' as IconProp} className="ms-1 ms-sm-2" />
          <ItemButton
            tootltip="Print"
            icon={'print' as IconProp}
            className="ms-1 ms-sm-2 d-none d-sm-inline-block"
          />
        </div>
        <Flex>
          <div className="d-none d-md-block">
            <small> 2 of 354</small>
            <IconButton
              variant="falcon-default"
              size="sm"
              icon="chevron-left"
              className="ms-2"
            />
            <IconButton
              variant="falcon-default"
              size="sm"
              icon="chevron-right"
              className="ms-2"
            />
          </div>
          <Dropdown className="font-sans-serif" align="end">
            <Dropdown.Toggle
              variant="falcon-default"
              size="sm"
              className="text-600 dropdown-caret-none ms-2"
            >
              <FontAwesomeIcon icon="cog" />
            </Dropdown.Toggle>
            <Dropdown.Menu className="py-2">
              <Dropdown.Item href="#/action-1">Configure inbox</Dropdown.Item>
              <Dropdown.Divider />
              <Dropdown.Item href="#/action-2">Settings</Dropdown.Item>
              <Dropdown.Item href="#/action-3">Themes</Dropdown.Item>
              <Dropdown.Divider />
              <Dropdown.Item href="#/action-4">Send feedback</Dropdown.Item>
              <Dropdown.Item href="#/action-4">Help</Dropdown.Item>
            </Dropdown.Menu>
          </Dropdown>
        </Flex>
      </Card.Body>
    </Card>
  );
};

export default EmailDetailHeader;
