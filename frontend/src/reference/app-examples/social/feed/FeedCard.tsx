
import FeedCardHeader from './FeedCardHeader';
import { Card } from 'react-bootstrap';
import FeedCardContent from './FeedCardContent';
import FeedCardFooter from './FeedCardFooter';

// Feed data types matching the raw feed.js structure
interface FeedUser {
  name: string;
  avatarSrc: string;
  time?: string;
  location?: string;
  privacy?: string;
  share?: string;
  status?: string;
}

interface FeedComment {
  id: string;
  avatarSrc: string;
  name: string;
  content: string;
  postTime: string;
}

interface FeedDetails {
  countLCS?: { like?: number; comment?: number; share?: number };
  reactions?: { like?: boolean; comment?: boolean; share?: boolean };
  comments?: FeedComment[];
  otherComments?: string;
}

interface FeedContent {
  status?: string;
  imgSrc?: string;
  gallery?: string[];
  feedEvent?: any;
  url?: any;
  video?: any;
}

interface Feed {
  id: string;
  user?: FeedUser;
  content?: FeedContent;
  details?: FeedDetails;
}

interface FeedCardProps {
  feed: Feed;
  [key: string]: any;
}

const FeedCard: React.FC<FeedCardProps> = ({ feed, ...rest }) => {
  const { id, user, content, details } = feed;

  return (
    <Card {...rest}>
      {!!user && <FeedCardHeader {...user} />}
      {!!content && <FeedCardContent {...content} />}
      {!!details && <FeedCardFooter id={id} {...details} />}
    </Card>
  );
};

export default FeedCard;
