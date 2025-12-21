import { Link } from 'react-router';

interface LCSTextProps {
  count: number;
  text: string;
}

const LCSText = ({ count, text }: LCSTextProps) => (
  <Link className="text-700 me-1" to="#!">
    {count} {text}
    {count !== 1 && 's'}
  </Link>
);

interface FeedReactionsProps {
  like?: number;
  comment?: number;
  share?: number;
}

const FeedReactions = ({ like, comment, share }: FeedReactionsProps) => {
  return (
    <div className="border-bottom border-200 fs-10 py-3">
      {!!like && <LCSText count={like} text="like" />}
      {!!comment && (
        <>
          • <LCSText count={comment} text="comment" />{' '}
        </>
      )}
      {!!share && (
        <>
          • <LCSText count={share} text="share" />{' '}
        </>
      )}
    </div>
  );
};

export default FeedReactions;
