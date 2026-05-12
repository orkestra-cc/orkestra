
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome';
import { Button, Col, Row, OverlayTrigger, Tooltip } from 'react-bootstrap';
import Flex from 'components/common/Flex';
import { useChatContext } from 'providers/ChatProvider';

interface Thread {
  id: number;
  userId?: number;
  groupId?: number;
  type: 'user' | 'group';
  messagesId: number;
  read: boolean;
}

interface ChatContentHeaderProps {
  thread: Thread;
  setHideSidebar: (hide: boolean) => void;
}

const ChatContentHeader = ({ thread, setHideSidebar }: ChatContentHeaderProps) => {
  const { getUser, isOpenThreadInfo, setIsOpenThreadInfo } = useChatContext();
  const user = getUser(thread);

  return (
    <div className="chat-content-header">
      <Row className="flex-between-center">
        <Col xs={6} md={8} as={Flex} alignItems="center">
          <div
            className="pe-3 text-700 d-md-none contacts-list-show cursor-pointer"
            onClick={() => setHideSidebar(true)}
          >
            <FontAwesomeIcon icon="chevron-left" />
          </div>
          <div className="min-w-0">
            <h5 className="mb-0 text-truncate fs-9">{user.name}</h5>
            <div className="fs-11 text-400">
              {'status' in user && user.status === 'status-online'
                ? 'Active on  chat'
                : 'Active 7h ago'}
            </div>
          </div>
        </Col>
        <Col xs="auto">
          <OverlayTrigger
            placement="top"
            overlay={
              <Tooltip style={{ position: 'fixed' }}>
                Start a Audio Call
              </Tooltip>
            }
          >
            <Button variant="orkestra-primary" className="me-2" size="sm">
              <FontAwesomeIcon icon="phone" />
            </Button>
          </OverlayTrigger>
          <OverlayTrigger
            placement="top"
            overlay={
              <Tooltip style={{ position: 'fixed' }}>
                Start a Video Call
              </Tooltip>
            }
          >
            <Button variant="orkestra-primary" className="me-2" size="sm">
              <FontAwesomeIcon icon="video" />
            </Button>
          </OverlayTrigger>
          <OverlayTrigger
            placement="top"
            overlay={
              <Tooltip style={{ position: 'fixed' }}>
                Conversation Information
              </Tooltip>
            }
          >
            <Button
              variant="orkestra-primary"
              size="sm"
              onClick={() => setIsOpenThreadInfo(!isOpenThreadInfo)}
            >
              <FontAwesomeIcon icon="info" />
            </Button>
          </OverlayTrigger>
        </Col>
      </Row>
    </div>
  );
};

export default ChatContentHeader;
