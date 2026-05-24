import { Card } from 'react-bootstrap';
import Background from 'components/common/Background';
import Avatar from 'components/common/Avatar';
import UserAvatar, { UserAvatarProfile } from 'components/common/UserAvatar';
import classNames from 'classnames';

interface ProfileBannerHeaderProps {
  // Pass either `user` (preferred — UserAvatar handles initials +
  // deterministic color fallback) OR `avatar` URL (legacy).
  user?: UserAvatarProfile;
  avatar?: string;
  coverSrc: string;
  className?: string;
}

const ProfileBannerHeader: React.FC<ProfileBannerHeaderProps> = ({
  user,
  avatar,
  coverSrc,
  className
}) => {
  return (
    <Card.Header
      className={classNames(className, 'position-relative mb-7')}
      style={{ height: '150px' }}
    >
      <Background image={coverSrc} className="rounded-3 rounded-bottom-0" />
      {user ? (
        <UserAvatar
          user={user}
          size="5xl"
          className="avatar-profile"
          mediaClass="img-thumbnail shadow-sm"
        />
      ) : (
        <Avatar
          size="5xl"
          className="avatar-profile"
          src={avatar}
          mediaClass="img-thumbnail shadow-sm"
        />
      )}
    </Card.Header>
  );
};

interface ProfileBannerBodyProps {
  children: React.ReactNode;
}

const ProfileBannerBody: React.FC<ProfileBannerBodyProps> = ({ children }) => {
  return <Card.Body>{children}</Card.Body>;
};

interface ProfileBannerProps {
  children: React.ReactNode;
}

const ProfileBanner: React.FC<ProfileBannerProps> & {
  Header: typeof ProfileBannerHeader;
  Body: typeof ProfileBannerBody;
} = ({ children }) => {
  return <Card className="mb-3">{children}</Card>;
};

ProfileBanner.Header = ProfileBannerHeader;
ProfileBanner.Body = ProfileBannerBody;

export default ProfileBanner;
