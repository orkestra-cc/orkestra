
import { Button } from 'react-bootstrap';
import PageHeader from 'components/common/PageHeader';
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome';
import OrkestraComponentCard from 'components/common/OrkestraComponentCard';
import EmojiPicker from 'emoji-picker-react';
import { getColor } from 'helpers/utils';
import { useAppContext } from 'providers/AppProvider';

const emojiPicker = `
  function EmojiPickerCode () {
    const [previewEmoji, setPreviewEmoji] = useState(false);
    const {
      config: { isDark }
    } = useAppContext();
    const [message, setMessage] = useState('');

    const addEmoji = (emojiData) => {
      setMessage(message + emojiData.emoji);
      setPreviewEmoji(false);
    };

    return (
      <div className="position-relative">
        <Button variant="info" onClick={() => setPreviewEmoji(!previewEmoji)}>
          <FontAwesomeIcon
            icon={['far', 'laugh-beam']}
            transform=""
          />
        </Button>

        {previewEmoji && (
          <EmojiPicker
            theme={isDark ? 'dark' : 'light'}
            onEmojiClick={addEmoji}
            skinTonesDisabled
            previewConfig={{ showPreview: false }}
            emojiStyle='google'
          />
        )}
      </div>
    );
  }
`;

const EmojiPickerExample = () => (
  <>
    <PageHeader
      title="Emoji Button"
      description="Emoji Picker React is a Slack-like customizable emoji picker component for React"
      className="mb-3"
    >
      <Button
        href="https://github.com/ealush/emoji-picker-react"
        target="_blank"
        variant="link"
        size="sm"
        className="ps-0"
      >
        Documentation for Emoji Button
        <FontAwesomeIcon icon="chevron-right" className="ms-1 fs-11" />
      </Button>
    </PageHeader>

    <OrkestraComponentCard noGuttersBottom>
      <OrkestraComponentCard.Header title="Example" />
      <OrkestraComponentCard.Body
        code={emojiPicker}
        scope={{ getColor, EmojiPicker, useAppContext, FontAwesomeIcon }}
        language="jsx"
      />
    </OrkestraComponentCard>
  </>
);

export default EmojiPickerExample;
