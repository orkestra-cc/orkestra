
import Flex from 'components/common/Flex';
import classNames from 'classnames';
import Avatar from 'components/common/Avatar';
import { Nav } from 'react-bootstrap';
import LastMessage from './LastMessage';
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome';
import ChatSidebarDropdownAction from './ChatSidebarDropdownAction';
import { useChatContext } from 'providers/ChatProvider';
import { IconProp } from '@fortawesome/fontawesome-svg-core';

interface Thread {
  id: number;
  userId?: number;
  groupId?: number;
  type: 'user' | 'group';
  messagesId: number;
  read: boolean;
}

interface ChatThreadProps {
  thread: Thread;
  index: number;
}

const ChatThread = ({ thread, index }: ChatThreadProps) => {
  const { getUser, messages } = useChatContext();
  const user = getUser(thread);
  const message = messages.find(({ id }) => id === thread.messagesId);
  const content = Array.isArray(message?.content) ? message.content : [];
  const lastMessage = content.length > 0 ? content[content.length - 1] : undefined;

  const getStatusIcon = (): IconProp => {
    if (!lastMessage || !('status' in lastMessage)) return 'check' as IconProp;
    const status = (lastMessage as any).status;
    if (status === 'seen' || status === 'sent') {
      return 'check' as IconProp;
    }
    return 'check-double' as IconProp;
  };

  return (
    <Nav.Link
      eventKey={index}
      className={classNames(`chat-contact hover-actions-trigger p-3`, {
        'unread-message': !thread.read,
        'read-message': thread.read
      })}
    >
      <div className="d-md-none d-lg-block">
        <ChatSidebarDropdownAction />
      </div>
      <Flex>
        <Avatar className={'status' in user ? user.status : undefined} src={user.avatarSrc} size="xl" />
        <div className="flex-1 chat-contact-body ms-2 d-md-none d-lg-block">
          <Flex justifyContent="between">
            <h6 className="mb-0 chat-contact-title">{user.name}</h6>
            <span className="message-time fs-11">
              {' '}
              {!!lastMessage && (lastMessage as any).time?.day}{' '}
            </span>
          </Flex>
          <div className="min-w-0">
            <div className="chat-contact-content pe-3">
              <LastMessage lastMessage={lastMessage as any} thread={thread} />
              <div className="position-absolute bottom-0 end-0 hover-hide">
                {!!lastMessage && 'status' in lastMessage && (lastMessage as any).status && (
                  <FontAwesomeIcon
                    icon={getStatusIcon()}
                    transform="shrink-5 down-4"
                    className={classNames({
                      'text-success': (lastMessage as any).status === 'seen',
                      'text-400':
                        (lastMessage as any).status === 'delivered' ||
                        (lastMessage as any).status === 'sent'
                    })}
                  />
                )}
              </div>
            </div>
          </div>
        </div>
      </Flex>
    </Nav.Link>
  );
};

export default ChatThread;
