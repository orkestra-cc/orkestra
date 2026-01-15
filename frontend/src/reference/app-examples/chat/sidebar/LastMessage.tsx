
import { users } from 'data/dashboard/default';
import createMarkup from 'helpers/createMarkup';

interface LastMessageData {
  senderUserId: number;
  messageType?: string;
  attachment?: string;
  message?: string;
}

interface Thread {
  id: number;
  type: 'user' | 'group';
}

interface LastMessageProps {
  lastMessage?: LastMessageData;
  thread: Thread;
}

const LastMessage = ({ lastMessage, thread }: LastMessageProps) => {
  const user = users.find(({ id }: { id: number }) => id === lastMessage?.senderUserId);
  const name = user?.name.split(' ');

  if (!lastMessage) {
    return <div>Say hi to your new friend</div>;
  }

  const lastMassagePreview =
    lastMessage.messageType === 'attachment'
      ? `${name?.[0] || 'User'} sent ${lastMessage.attachment}`
      : lastMessage.message?.split('<br>') || [];

  if (lastMessage.senderUserId === 3) {
    return `You: ${Array.isArray(lastMassagePreview) ? lastMassagePreview[0] : lastMassagePreview}`;
  }

  if (thread.type === 'group') {
    return (
      <div
        className="chat-contact-content"
        dangerouslySetInnerHTML={createMarkup(
          `${name?.[0] || 'User'}: ${Array.isArray(lastMassagePreview) ? lastMassagePreview.join('') : lastMassagePreview}`
        )}
      />
    );
  }

  return (
    <div
      className="chat-contact-content"
      dangerouslySetInnerHTML={createMarkup(
        Array.isArray(lastMassagePreview) ? lastMassagePreview.join('') : lastMassagePreview
      )}
    />
  );
};

export default LastMessage;
