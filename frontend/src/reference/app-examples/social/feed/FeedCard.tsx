
import FeedCardHeader from './FeedCardHeader';
import { Card } from 'react-bootstrap';
import FeedCardContent from './FeedCardContent';
import FeedCardFooter from './FeedCardFooter';

// Type definitions for Social features
interface SocialUser {
  id: string;
  name: string;
  avatarSrc: string;
  username?: string;
  isOnline?: boolean;
  lastActivity?: string;
}

interface SocialFeedContent {
  text?: string;
  images?: string[];
  video?: string;
  link?: SocialFeedLink;
  poll?: SocialPoll;
}

interface SocialFeedLink {
  url: string;
  title: string;
  description: string;
  image?: string;
}

interface SocialPoll {
  question: string;
  options: SocialPollOption[];
  totalVotes: number;
  expiresAt?: string;
}

interface SocialPollOption {
  id: string;
  text: string;
  votes: number;
  percentage: number;
}

interface SocialFeedDetails {
  likes: number;
  comments: number;
  shares: number;
  likedByCurrentUser?: boolean;
  timestamp: string;
}

interface SocialFeed {
  id: string;
  user: SocialUser;
  content: SocialFeedContent;
  details: SocialFeedDetails;
  type: 'post' | 'share' | 'event' | 'poll';
}

interface FeedCardProps {
  feed: SocialFeed;
  [key: string]: any;
}

const FeedCard: React.FC<FeedCardProps> = ({ feed, ...rest }) => {
  const { id, user, content, details } = feed;

  // Map SocialFeed structure to component props
  const userProps = user ? {
    status: user.isOnline ? 'status-online' : 'status-offline',
    avatarSrc: user.avatarSrc,
    time: details.timestamp,
    name: user.name,
    share: feed.type === 'share' ? 'post' : undefined,
    location: user.lastActivity || '',
    privacy: 'public'
  } : undefined;

  const contentProps = content ? {
    status: content.text,
    imgSrc: content.images?.[0],
    gallery: content.images,
    feedEvent: undefined,
    url: content.link,
    video: content.video ? { src: content.video } : undefined
  } : undefined;

  const detailsProps = details ? {
    id,
    countLCS: { like: details.likes, comment: details.comments, share: details.shares },
    reactions: { like: details.likedByCurrentUser || false, comment: false, share: false },
    comments: [],
    otherComments: ''
  } : undefined;

  return (
    <Card {...rest}>
      {!!userProps && <FeedCardHeader {...userProps} />}
      {!!contentProps && <FeedCardContent {...contentProps} />}
      {!!detailsProps && <FeedCardFooter {...detailsProps} />}
    </Card>
  );
};

export default FeedCard;
