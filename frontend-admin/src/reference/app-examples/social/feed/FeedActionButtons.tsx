import { useState } from 'react';
import { Col, Row } from 'react-bootstrap';
import Flex from 'components/common/Flex';
import LikeInactive from 'assets/img/illustrations/like-inactive.png';
import commentActive from 'assets/img/illustrations/comment-active.png';
import shareActive from 'assets/img/illustrations/share-active.png';
import shareInactive from 'assets/img/illustrations/share-inactive.png';
import commentInActive from 'assets/img/illustrations/comment-inactive.png';
import likeActive from 'assets/img/illustrations/like-active.png';
import { useFeedContext } from 'providers/FeedProvider';

interface FeedActionButtonsProps {
  id: string | number;
  reactions?: {
    like?: boolean;
    comment?: boolean;
    share?: boolean;
  };
}

const FeedActionButtons = ({ id, reactions }: FeedActionButtonsProps) => {
  const [liked, setLiked] = useState(reactions?.like);
  const { feeds, feedDispatch } = useFeedContext();

  const toggleLiked = () => {
    setLiked(!liked);
    const feed = feeds.find(item => item.id === id);
    if (!feed) return;

    feed.details.reactions.like = !liked;
    feed.details.countLCS.like = !liked
      ? feed.details.countLCS.like + 1
      : feed.details.countLCS.like - 1;

    feedDispatch({
      type: 'UPDATE',
      payload: { id: typeof id === 'string' ? parseInt(id, 10) : id, feed }
    });
  };
  return (
    <Row className="g-0 fw-semibold text-center py-2 fs-10">
      <Col xs="auto">
        <Flex
          alignItems="center"
          className="rounded text-700 me-3 cursor-pointer"
          onClick={toggleLiked}
        >
          <img
            src={reactions?.like ? likeActive : LikeInactive}
            alt=""
            width="20"
          />
          <span className="ms-1">Like</span>
        </Flex>
      </Col>
      <Col xs="auto">
        <Flex
          alignItems="center"
          className="rounded text-700 me-3 cursor-pointer"
        >
          <img
            src={reactions?.comment ? commentActive : commentInActive}
            alt=""
            width="20"
          />
          <span className="ms-1">comment</span>
        </Flex>
      </Col>
      <Col xs="auto">
        <Flex
          alignItems="center"
          className="rounded text-700 me-3 cursor-pointer"
        >
          <img
            src={reactions?.share ? shareActive : shareInactive}
            alt=""
            width="20"
          />
          <span className="ms-1">share</span>
        </Flex>
      </Col>
    </Row>
  );
};


export default FeedActionButtons;
