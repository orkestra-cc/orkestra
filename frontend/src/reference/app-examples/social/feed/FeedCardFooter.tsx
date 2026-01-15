import { useState } from 'react';
import { Card, Form } from 'react-bootstrap';
import { v4 as uuid } from 'uuid';
import av3 from 'assets/img/team/3.jpg';
import FeedReactions from './FeedReactions';
import FeedActionButtons from './FeedActionButtons';
import Flex from 'components/common/Flex';
import Avatar from 'components/common/Avatar';
import Comments from './Comments';
import { useFeedContext } from 'providers/FeedProvider';

interface FeedCardFooterProps {
  id: string | number;
  countLCS?: { like?: number; comment?: number; share?: number };
  reactions?: {
    like?: boolean;
    comment?: boolean;
    share?: boolean;
  };
  comments?: any[];
  otherComments?: string;
}

const FeedCardFooter = ({
  id,
  countLCS = { like: 0 },
  reactions,
  comments = [],
  otherComments
}: FeedCardFooterProps) => {
  const { feeds, feedDispatch } = useFeedContext();
  const [comment, setComment] = useState('');

  const submitComment = (e: React.FormEvent<HTMLFormElement>) => {
    e.preventDefault();
    const newComment = {
      id: uuid(),
      avatarSrc: av3,
      name: 'Rebecca Marry',
      content: comment,
      postTime: '1m'
    };

    const feed = feeds.find(feed => feed.id === id);
    if (!feed) return;

    feed.details.reactions.comment = true;
    feed.details.comments = [newComment, ...comments];
    feedDispatch({ type: 'UPDATE', payload: { id: typeof id === 'string' ? parseInt(id, 10) : id, feed } });
    setComment('');
  };

  return (
    <Card.Footer className="bg-body-tertiary pt-0">
      <FeedReactions {...countLCS} />
      <FeedActionButtons id={id} reactions={reactions} />
      <Form onSubmit={submitComment}>
        <Flex alignItems="center" className="border-top border-200 pt-3">
          <Avatar src={av3} size="xl" />
          <Form.Control
            type="text"
            className="rounded-pill ms-2 fs-10"
            placeholder="Write a comment..."
            value={comment}
            onChange={e => setComment(e.target.value)}
          />
        </Flex>
      </Form>
      {!!comments && (
        <Comments comments={comments} loadComment={otherComments} />
      )}
    </Card.Footer>
  );
};

export default FeedCardFooter;
