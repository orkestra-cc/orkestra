import classNames from 'classnames';
import Avatar, { AvatarRounded, AvatarSize } from './Avatar';
import { avatarColor, initialsFor } from 'helpers/avatarColor';

// UserAvatar is the canonical avatar renderer for user identities
// across the operator console. It respects the backend's avatar-source
// preference:
//
//   * a non-empty `avatar` URL renders as <img> (handles uploaded /
//     oauth_* / pre-AvatarSource legacy users uniformly — the backend
//     has already resolved the right URL).
//   * an empty `avatar` renders initials over a deterministic color
//     derived from the user's most stable identifier (UUID > email
//     > fullName) so the same user always sees the same color in the
//     navbar, profile banner, and dropdown.
//
// Wraps the existing <Avatar> primitive for the image case so the
// Bootstrap-aware sizing + `rounded` modifiers stay consistent. The
// initials case is rendered inline with the deterministic color
// because the underlying primitive defers initials color to CSS,
// which would render every user with the same hue.

export interface UserAvatarProfile {
  id?: string;
  email?: string;
  fullName?: string;
  username?: string;
  avatar?: string;
}

interface UserAvatarProps {
  user?: UserAvatarProfile | null;
  size?: AvatarSize;
  rounded?: AvatarRounded;
  className?: string;
  mediaClass?: string;
  // emptyName forces initials="?" when there's no name and no email.
  // Useful in tests / loading placeholders.
  emptyName?: string;
}

const UserAvatar: React.FC<UserAvatarProps> = ({
  user,
  size = 'xl',
  rounded = 'circle',
  className,
  mediaClass,
  emptyName
}) => {
  if (user?.avatar) {
    return (
      <Avatar
        src={user.avatar}
        size={size}
        rounded={rounded}
        className={className}
        mediaClass={mediaClass}
      />
    );
  }

  const seed =
    user?.id || user?.email || user?.fullName || user?.username || '';
  const initials = initialsFor(
    user?.fullName || user?.username,
    user?.email || emptyName
  );
  const { background, color } = avatarColor(seed);

  const avatarClassNames = classNames('avatar', `avatar-${size}`, className);
  const mediaClasses = classNames(
    rounded ? `rounded-${rounded}` : 'rounded',
    mediaClass
  );

  return (
    <div className={avatarClassNames}>
      <div
        className={`avatar-name ${mediaClasses}`}
        style={{ backgroundColor: background, color }}
        aria-label={user?.fullName || user?.email || initials}
      >
        <span>{initials}</span>
      </div>
    </div>
  );
};

export default UserAvatar;
