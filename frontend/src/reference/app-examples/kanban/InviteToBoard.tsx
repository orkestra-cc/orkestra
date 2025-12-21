import { useRef, useState } from 'react';
import {
  Dropdown,
  Form,
  OverlayTrigger,
  Tooltip,
  Button
} from 'react-bootstrap';
import Flex from 'components/common/Flex';
import IconButton from 'components/common/IconButton';
import { copyToClipBoard } from 'helpers/utils';
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome';
import { useAppContext } from 'providers/AppProvider';

const InviteToBoard = () => {
  const [tooltipText, setTooltipText] = useState('Copy link to invite');
  const copyTextRef = useRef<HTMLInputElement | HTMLTextAreaElement>(null);
  const {
    config: { isRTL }
  } = useAppContext();

  const handleCopyText = () => {
    copyToClipBoard(copyTextRef as any);
    setTooltipText('Copied to Clipboard!');
  };

  return (
    <Dropdown>
      <Dropdown.Toggle
        variant="falcon-default"
        size="sm"
        className="dropdown-caret-none me-2"
      >
        <FontAwesomeIcon icon="plus" className="me-md-2" />
        <span className="d-none d-md-inline-block">Invite</span>
      </Dropdown.Toggle>
      <Dropdown.Menu
        align={isRTL ? 'start' : 'end'}
        className="pt-2 pb-0 text-nowrap end-0"
        style={{ minWidth: '300px' }}
      >
        <h6 className="dropdown-header text-center ls">Invite To Board</h6>
        <Dropdown.Divider className="mb-0" />
        <div className="p-3">
          <Form>
            <div className="border rounded-1 fs-11 mb-3">
              <Flex
                alignItems="center"
                justifyContent="between"
                className="border-bottom bg-200"
              >
                <div className="px-2 text-700">
                  Anyone with the link can join
                </div>
                <div className="border-start">
                  <OverlayTrigger
                    trigger={['hover', 'click']}
                    placement="top"
                    // trigger="hover"
                    overlay={
                      <Tooltip style={{ position: 'fixed' }}>
                        {tooltipText}
                      </Tooltip>
                    }
                  >
                    <div>
                      <IconButton
                        onMouseLeave={() =>
                          setTooltipText('Copy link to invite')
                        }
                        onClick={handleCopyText}
                        variant="link"
                        size="sm"
                        icon={['far', 'copy']}
                        className="text-decoration-none hover-primary rounded-0 fs-11"
                        iconClassName="me-2"
                      >
                        Copy link
                      </IconButton>
                    </div>
                  </OverlayTrigger>
                </div>
              </Flex>
              <Form.Control
                type="text"
                ref={copyTextRef as any}
                readOnly
                defaultValue="https://prium.github.io/falcon/kanban/QhNCShh8TdxKx0kYN1oWzzKJDjOYUXhm9IJ035laUVdWMYsUN5"
                className="bg-white dark__bg-dark border-0 fs-11 px-1 rounded-top-0"
              />
            </div>
            <Form.Control
              size="sm"
              type="text"
              placeholder="Enter name or email"
            />
            <Button variant="primary" size="sm" className="d-block w-100 mt-2">
              Send Invitation
            </Button>
          </Form>
        </div>
      </Dropdown.Menu>
    </Dropdown>
  );
};

export default InviteToBoard;
