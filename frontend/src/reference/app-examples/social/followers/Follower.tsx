import { Link } from 'react-router';
import { Image } from 'react-bootstrap';
import paths from 'routes/paths';

interface FollowerProps {
  follower: {
    avatarSrc: string;
    name: string;
    institution: string;
  };
}

const Follower = ({ follower }: FollowerProps) => {
  const { avatarSrc, name, institution } = follower;
  return (
    <div className="bg-white dark__bg-1100 p-3 h-100">
      <Link to={paths.userProfile}>
        <Image
          thumbnail
          fluid
          roundedCircle
          className="mb-3 shadow-sm"
          src={avatarSrc}
          width={100}
        />
      </Link>
      <h6 className="mb-1">
        <Link to={paths.userProfile}>{name}</Link>
      </h6>
      <p className="fs-11 mb-1">
        <Link className="text-700" to="#!">
          {institution}
        </Link>
      </p>
    </div>
  );
};

export default Follower;
