import Flex from 'components/common/Flex';
import Avatar from 'components/common/Avatar';
import { Link } from 'react-router';
import createMarkup from 'helpers/createMarkup';
import paths from 'routes/paths';

interface Comment {
  id: string;
  avatarSrc: string;
  name: string;
  content: string;
  postTime: string;
}

interface CommentsProps {
  comments: Comment[];
  loadComment?: string;
}

const Comments = ({ comments, loadComment }: CommentsProps) => {
  return (
    <>
      {comments.map(({ id, avatarSrc, name, content, postTime }: Comment) => (
        <div key={id}>
          <Flex className="mt-3">
            <Link to={paths.userProfile}>
              <Avatar src={avatarSrc} size="xl" />
            </Link>
            <div className="flex-1 ms-2 fs-10">
              <p className="mb-1 bg-200 rounded-3 p-2">
                <Link className="fw-semibold" to={paths.userProfile}>
                  {name}
                </Link>
                <span
                  className="ms-1"
                  dangerouslySetInnerHTML={createMarkup(content)}
                />
              </p>
              <div className="px-2">
                <a href="#!">Like</a> • <a href="#!">Reply</a> • {postTime}
              </div>
            </div>
          </Flex>
        </div>
      ))}
      {!!loadComment && (
        <a className="fs-10 text-700 d-inline-block mt-2" href="#!">
          Load more comments ({loadComment})
        </a>
      )}
    </>
  );
};

export default Comments;
