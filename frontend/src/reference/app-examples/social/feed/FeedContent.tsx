
import { useFeedContext } from 'providers/FeedProvider';
import CreatePost from './CreatePost';
import FeedCard from './FeedCard';
import classNames from 'classnames';

const FeedContent = () => {
  const { feeds } = useFeedContext();
  return (
    <>
      <CreatePost className="mb-3" />
      {feeds.map((feed: any, index: number) => (
        <FeedCard
          key={feed.id}
          feed={feed}
          className={classNames({
            'mb-3': index + 1 !== feeds.length,
            'mb-3 mb-lg-0': index + 1 === feeds.length
          })}
        />
      ))}
    </>
  );
};

export default FeedContent;
