// Inline avatar used by PersonsTable + OrganizationsTable.
//
// Renders the existing `Avatar` primitive in size "s". When an email is
// available we fetch a Gravatar (with identicon fallback so unregistered
// emails still get a deterministic glyph). While the SHA-256 digest is
// being computed (or when no email exists) we fall back to the contact's
// initials via Avatar's built-in `name` mode.

import Avatar from 'components/common/Avatar';
import { useGravatarUrl } from 'utils/gravatar';

interface ContactAvatarProps {
  email?: string;
  name: string;
}

const ContactAvatar = ({ email, name }: ContactAvatarProps) => {
  const src = useGravatarUrl(email, { size: 64, fallback: 'identicon' });
  return <Avatar size="s" src={src} name={name || '?'} />;
};

export default ContactAvatar;
