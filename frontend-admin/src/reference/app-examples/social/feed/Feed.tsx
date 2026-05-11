
import { Col, Row } from 'react-bootstrap';
import events from 'data/events/events';
import BirthdayNotice from './BirthdatNotice';
import AddToFeed from './AddToFeed';
import Events from 'pages/user/profile/Events';
import FeedContent from './FeedContent';
import FeedProvider from 'providers/FeedProvider';

const Feed: React.FC = () => {
  return (
    <FeedProvider>
      <Row className="g-0 g-lg-3">
        <Col lg={8}>
          <FeedContent />
        </Col>
        <Col lg={4}>
          <BirthdayNotice
            name="Emma Watson"
            profileLink="/user/profile"
            className="mb-3"
          />
          <AddToFeed />
          <Events cardTitle="You may interested" events={events.slice(2, 5)} />
        </Col>
      </Row>
    </FeedProvider>
  );
};

export default Feed;
