import { Link } from 'react-router';
import { Card, Col, Row } from 'react-bootstrap';
import ProfileBanner from '../ProfileBanner';
import coverSrc from 'assets/img/generic/4.jpg';
import avatar from 'assets/img/team/2.jpg';
import ProfileSettings from './ProfileSettings';
import ExperiencesSettings from './ExperiencesSettings';
import EducationSettings from './EducationSettings';
import AccountSettings from './AccountSettings';
import BillingSettings from './BillingSettings';
import DangerZone from './DangerZone';
import paths from 'routes/paths';

// Account settings page. Password change and MFA management moved to
// the dedicated /user/security page (one source of truth so the
// suspicious-login email's deep link is unambiguous and the page
// stays focused on profile / preferences).
const Settings: React.FC = () => {
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
            <AccountSettings />
            <BillingSettings />
            <Card className="mb-3">
              <Card.Body>
                <h5 className="mb-2">Account security</h5>
                <p className="fs-10 text-muted mb-3">
                  Password, two-factor, sessions, and trusted devices live on
                  their own page now.
                </p>
                <Link
                  to={paths.userSecurity}
                  className="btn btn-outline-primary btn-sm"
                >
                  Manage security
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
