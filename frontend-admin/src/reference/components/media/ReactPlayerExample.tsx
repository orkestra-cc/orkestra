
import PageHeader from 'components/common/PageHeader';
import FalconComponentCard from 'components/common/FalconComponentCard';
import { Button } from 'react-bootstrap';
import ReactPlayer from 'react-player';
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome';

const youtubeCode = `<ReactPlayer
  src="https://www.youtube.com/embed/zpOULjyy-n8?rel=0"
  controls={true}
  className="react-player"
  width="100%"
  height="100%"
/>`;
const vimeoCode = `<ReactPlayer
  src="https://vimeo.com/90509568"
  controls={true}
  className="react-player"
  width="100%"
  height="100%"
/>`;

const ReactPlayerExample = () => (
  <>
    <PageHeader
      title="React Player"
      description="A React component for playing a variety of URLs, including file paths, YouTube, Facebook, Twitch, SoundCloud, Streamable, Vimeo, Wistia, Mixcloud, DailyMotion and Kaltura."
      className="mb-3"
    >
      <Button
        href="https://github.com/CookPete/react-player"
        target="_blank"
        variant="link"
        size="sm"
        className="ps-0"
      >
        React Player Documentation
        <FontAwesomeIcon icon="chevron-right" className="ms-1 fs-11" />
      </Button>
    </PageHeader>

    <FalconComponentCard>
      <FalconComponentCard.Header title="Youtube player" light={false} />
      <FalconComponentCard.Body
        code={youtubeCode}
        language="jsx"
        scope={{ ReactPlayer }}
      />
    </FalconComponentCard>

    <FalconComponentCard noGuttersBottom>
      <FalconComponentCard.Header title="Vimeo player" light={false} />
      <FalconComponentCard.Body
        code={vimeoCode}
        language="jsx"
        scope={{ ReactPlayer }}
      />
    </FalconComponentCard>
  </>
);

export default ReactPlayerExample;
