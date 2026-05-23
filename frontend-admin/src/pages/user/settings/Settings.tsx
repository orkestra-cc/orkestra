import { Link } from 'react-router';
import { Card, Col, Row } from 'react-bootstrap';
import { useTranslation } from 'react-i18next';
import ProfileBanner from '../ProfileBanner';
import coverSrc from 'assets/img/generic/4.jpg';
import avatar from 'assets/img/team/2.jpg';
import ProfileSettings from './ProfileSettings';
import ExperiencesSettings from './ExperiencesSettings';
import EducationSettings from './EducationSettings';
import LanguageSettings from './LanguageSettings';
import AccountSettings from './AccountSettings';
import BillingSettings from './BillingSettings';
import DangerZone from './DangerZone';
import paths from 'routes/paths';

// Account settings page. Password change and MFA management moved to
// the dedicated /user/security page (one source of truth so the
// suspicious-login email's deep link is unambiguous and the page
// stays focused on profile / preferences).
const Settings: React.FC = () => {
  const { t } = useTranslation();
  return (
    <>
      <ProfileBanner>
        <ProfileBanner.Header
          coverSrc={coverSrc}
          avatar={avatar}
          className="mb-8"
        />
      </ProfileBanner>
      <Row className="g-3">
        <Col lg={8}>
          <ProfileSettings />
          <ExperiencesSettings />
          <EducationSettings />
        </Col>
        <Col lg={4}>
          <div className="sticky-sidebar">
            <LanguageSettings />
            <AccountSettings />
            <BillingSettings />
            <Card className="mb-3">
              <Card.Body>
                <h5 className="mb-2">{t('settings.security.title')}</h5>
                <p className="fs-10 text-muted mb-3">
                  {t('settings.security.description')}
                </p>
                <Link
                  to={paths.userSecurity}
                  className="btn btn-outline-primary btn-sm"
                >
                  {t('settings.security.manage')}
                </Link>
              </Card.Body>
            </Card>
            <DangerZone />
          </div>
        </Col>
      </Row>
    </>
  );
};

export default Settings;
