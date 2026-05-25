import { Card, Col, Row } from 'react-bootstrap';
import { useTranslation } from 'react-i18next';
import UserAvatar from 'components/common/UserAvatar';
import ProfileSettings from './ProfileSettings';
import LanguageSettings from './LanguageSettings';
import AvatarSettings from './AvatarSettings';
import SecuritySummaryCard from './SecuritySummaryCard';
import DangerZone from './DangerZone';
import { useGetCurrentUserQuery } from 'store/api/authApi';
import { avatarColor } from 'helpers/avatarColor';

// SettingsHero — compact identity banner with a deterministic two-tone
// gradient seeded off the user's stable identifier (uuid > email).
// Same seed as the initials-avatar palette, so an account that falls
// back to initials sees its banner and avatar share a hue. Avoids a
// stock cover image without requiring a new "cover upload" pipeline.
const SettingsHero: React.FC = () => {
  const { t } = useTranslation();
  const { data: user } = useGetCurrentUserQuery();

  const seed = user?.id ?? user?.email ?? '';
  const { background } = avatarColor(seed);

  return (
    <Card
      className="mb-3 overflow-hidden border-0"
      style={{
        background: `linear-gradient(135deg, ${background} 0%, ${background}99 60%, ${background}33 100%)`
      }}
    >
      <Card.Body className="d-flex align-items-center gap-3 py-4 text-white">
        <UserAvatar
          user={user ?? undefined}
          size="4xl"
          mediaClass="img-thumbnail shadow-sm border-2 border-white"
        />
        <div className="flex-1">
          <h3 className="text-white mb-1">
            {user?.fullName || user?.email || '—'}
          </h3>
          <div className="opacity-75 fs-9">
            {user?.email}
            {user?.role && (
              <span className="ms-2">
                ·{' '}
                {t(`adminUsers.roles.${user.role}`, {
                  defaultValue: user.role
                })}
              </span>
            )}
          </div>
        </div>
      </Card.Body>
    </Card>
  );
};

// Settings page — self-service preferences. Password / MFA / sessions
// live on the dedicated /user/security page so the suspicious-login
// email's deep link stays unambiguous and this page stays focused on
// profile and preferences. The SecuritySummaryCard surfaces a live
// read of /me/auth-methods so the user can scan their posture without
// jumping pages.
const Settings: React.FC = () => {
  return (
    <>
      <SettingsHero />
      <Row className="g-3">
        <Col lg={8}>
          <ProfileSettings />
        </Col>
        <Col lg={4}>
          <div className="sticky-sidebar">
            <AvatarSettings />
            <LanguageSettings />
            <SecuritySummaryCard />
            <DangerZone />
          </div>
        </Col>
      </Row>
    </>
  );
};

export default Settings;
