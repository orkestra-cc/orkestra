import { useEffect, useState } from 'react';
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome';
import classNames from 'classnames';
import { useChatContext } from 'providers/ChatProvider';
import EmojiPicker, { Theme, EmojiStyle } from 'emoji-picker-react';
import { Button, Form } from 'react-bootstrap';
import TextareaAutosize from 'react-textarea-autosize';
import { useAppContext } from 'providers/AppProvider';

interface EmojiClickData {
  emoji: string;
}

const formatDate = (date: Date) => {
  const options: Intl.DateTimeFormatOptions = {
    weekday: 'short',
    day: 'numeric',
    month: 'long',
    year: 'numeric',
    hour: 'numeric',
    minute: 'numeric'
  };

  const now = date
    .toLocaleString('en-US', options)
    .split(',')
    .map(item => item.trim());

  return {
    day: now[0],
    hour: now[3],
    date: now[1] + ', ' + now[2]
  };
};

const MessageTextArea = () => {
  const {
    messagesDispatch,
    messages,
    threadsDispatch,
    currentThread,
    setScrollToBottom,
    isOpenThreadInfo
  } = useChatContext();
  const [previewEmoji, setPreviewEmoji] = useState(false);
  const [message, setMessage] = useState('');

  const {
    config: { isDark }
  } = useAppContext();

  const addEmoji = (emojiObject: EmojiClickData) => {
    setMessage(prev => prev + emojiObject.emoji);
    setPreviewEmoji(false);
  };


  const handleSubmit = (e: React.FormEvent) => {
    e.preventDefault();
    const date = new Date();
    const newMessage = {
      senderUserId: 3,
      message: `${message.replace(/(?:\r\n|\r|\n)/g, '<br>')}`,
      status: 'delivered',
      time: formatDate(date)
    };

    const messageObj = messages.find(
      ({ id }) => id === currentThread.messagesId
    );
    const content = messageObj?.content || [];

    if (message) {
      messagesDispatch({
        type: 'EDIT',
        payload: {
          id: currentThread.messagesId,
          content: [...content, newMessage]
        },
        id: currentThread.messagesId
      });

      threadsDispatch({
        type: 'EDIT',
        payload: currentThread,
        id: currentThread.id,
        isUpdatedStart: true
      });
    }
    setMessage('');
    setScrollToBottom(true);
  };

  useEffect(() => {
    if (isOpenThreadInfo) {
      setPreviewEmoji(false);
    }
  }, [isOpenThreadInfo]);

  return (
    <Form className="chat-editor-area" onSubmit={handleSubmit}>
      <TextareaAutosize
        minRows={1}
        maxRows={6}
        value={message}
        placeholder="Type your message"
        onChange={({ target }) => setMessage(target.value)}
        className="form-control outline-none resize-none rounded-0 border-0 emojiarea-editor"
      />

      <Form.Group controlId="chatFileUpload">
        <Form.Label className="chat-file-upload cursor-pointer">
          <FontAwesomeIcon icon="paperclip" />
        </Form.Label>
        <Form.Control type="file" className="d-none" />
      </Form.Group>

      <Button
        variant="link"
        className="emoji-icon "
        onClick={() => setPreviewEmoji(!previewEmoji)}
      >
        <FontAwesomeIcon
          icon={['far', 'laugh-beam']}
          onClick={() => setPreviewEmoji(!previewEmoji)}
        />
      </Button>

      {previewEmoji && (
        <div className="chat-emoji-picker" dir="ltr">
          <EmojiPicker
            onEmojiClick={addEmoji}
            theme={(isDark ? 'dark' : 'light') as Theme}
            skinTonesDisabled={true}
            previewConfig={{ showPreview: false }}
            emojiStyle={'google' as EmojiStyle}
            width={354}
            height={435}

          />
        </div>
      )}

      <Button
        variant="send"
        size="sm"
        className={classNames('shadow-none', {
          'text-primary': message.length > 0
        })}
        type="submit"
      >
        Send
      </Button>
    </Form>
  );
};


export default MessageTextArea;
