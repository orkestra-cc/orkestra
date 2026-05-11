import { useEffect, useRef } from 'react';
import ChatContentBodyIntro from './ChatContentBodyIntro';
import Message from './Message';
import SimpleBar from 'simplebar-react';
import ThreadInfo from './ThreadInfo';
import { useChatContext } from 'providers/ChatProvider';

interface Thread {
  id: number;
  userId?: number;
  groupId?: number;
  type: 'user' | 'group';
  messagesId: number;
  read: boolean;
}

interface ChatContentBodyProps {
  thread: Thread;
}

const ChatContentBody = ({ thread }: ChatContentBodyProps) => {
  let lastDate: string | null = null;
  const messagesEndRef = useRef<HTMLDivElement>(null);

  const { getUser, messages, scrollToBottom, setScrollToBottom } =
    useChatContext();
  const user = getUser(thread);
  const messageObj = messages.find(({ id }) => id === thread.messagesId);
  const content = Array.isArray(messageObj?.content) ? messageObj.content : [];

  useEffect(() => {
    if (scrollToBottom && messagesEndRef.current) {
      setTimeout(() => {
        messagesEndRef.current?.scrollIntoView({ behavior: 'smooth' });
      }, 500);
      setScrollToBottom(false);
    }
  }, [scrollToBottom, setScrollToBottom]);

  return (
    <div className="chat-content-body" style={{ display: 'inherit' }}>
      <ThreadInfo thread={thread} />
      <SimpleBar style={{ height: '100%' }}>
        <div className="chat-content-scroll-area">
          <ChatContentBodyIntro user={user} isGroup={thread.type === 'group'} />
          {content.map((msg: any, index: number) => {
            const { message, time, senderUserId, status } = msg;
            const showDate = lastDate !== time.date;
            lastDate = time.date;

            return (
              <div key={index}>
                {showDate && (
                  <div className="text-center fs-11 text-500">{`${time.date}, ${time.hour}`}</div>
                )}
                <Message
                  message={message}
                  senderUserId={senderUserId}
                  time={time}
                  status={status}
                  isGroup={thread.type === 'group'}
                />
              </div>
            );
          })}
        </div>
        <div ref={messagesEndRef} />
      </SimpleBar>
    </div>
  );
};


export default ChatContentBody;
