
import { Nav } from 'react-bootstrap';
import ChatThread from './ChatThread';
import SimpleBar from 'simplebar-react';
import ChatContactsSearch from './ChatContactSearch';
import classNames from 'classnames';
import { useChatContext } from 'providers/ChatProvider';

interface ChatSidebarProps {
  hideSidebar: boolean;
}

const ChatSidebar = ({ hideSidebar }: ChatSidebarProps) => {
  const { threads } = useChatContext();
  return (
    <div className={classNames('chat-sidebar', { 'start-0': hideSidebar })}>
      <div className="contacts-list">
        <SimpleBar style={{ height: '100%', minWidth: '65px' }}>
          <Nav className="border-0">
            {threads.map((thread, index) => (
              <ChatThread thread={thread} index={index} key={thread.id} />
            ))}
          </Nav>
        </SimpleBar>
      </div>
      <ChatContactsSearch />
    </div>
  );
};

export default ChatSidebar;
