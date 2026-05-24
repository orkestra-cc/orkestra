import { FontAwesomeIcon } from '@fortawesome/react-fontawesome';
import coverSrc from 'assets/img/generic/4.jpg';
import googleLogo from 'assets/img/logos/g.png';
import appleLogo from 'assets/img/logos/apple.png';
import githubLogo from 'assets/img/logos/github.png';
import Flex from 'components/common/Flex';
import VerifiedBadge from 'components/common/VerifiedBadge';
import { Alert, Col, Row, Spinner } from 'react-bootstrap';
import { useTranslation } from 'react-i18next';
import { Link } from 'react-router';
import { OAuthProvider, useGetCurrentUserQuery } from 'store/api/authApi';
import paths from 'routes/paths';
import ProfileBanner from '../ProfileBanner';

// /user/profile banner — rewired to render the live authenticated user.
// Picks the avatar via the shared ProfileBanner.Header user= path so
// initials + deterministic color kick in when the user has no image.
// The right-side column shows actually-linked OAuth providers (or
// nothing when none are linked) instead of the Falcon mock list.
//
// The rest of `Profile.tsx` (Followers, Education, Photos, ...) is
// still Falcon scaffold — out of scope for this PR.

const PROVIDER_LOGOS: Partial<Record<OAuthProvider, string>> = {
  google: googleLogo,
  apple: appleLogo,
  github: githubLogo
};

const PROVIDER_LABELS: Record<OAuthProvider, string> = {
  google: 'Google',
  apple: 'Apple',
  github: 'GitHub',
  discord: 'Discord'
};

const Banner: React.FC = () => {
  const { t } = useTranslation();
  const { data: user, isLoading, error } = useGetCurrentUserQuery();

  if (isLoading) {
    return (
      <ProfileBanner>
        <ProfileBanner.Header coverSrc={coverSrc} className="mb-8" />
        <ProfileBanner.Body>
          <div className="d-flex justify-content-center py-3">
            <Spinner animation="border" role="status">
              <span className="visually-hidden">
                {t('profileShared.loadingAria')}
              </span>
            </Spinner>
          </div>
        </ProfileBanner.Body>
      </ProfileBanner>
    );
  }

  if (error || !user) {
    return (
      <ProfileBanner>
        <ProfileBanner.Header coverSrc={coverSrc} className="mb-8" />
        <ProfileBanner.Body>
          <Alert variant="warning" className="mb-0">
            {t('profileShared.userNotFound')}
          </Alert>
        </ProfileBanner.Body>
      </ProfileBanner>
    );
  }

  const providers = user.oauthProviders ?? [];

  return (
    <ProfileBanner>
      <ProfileBanner.Header user={user} coverSrc={coverSrc} />
      <ProfileBanner.Body>
        <Row>
          <Col lg={8}>
            <h4 className="mb-1">
              {user.fullName || user.email}{' '}
              {user.emailVerified && <VerifiedBadge />}
            </h4>
            <h5 className="fs-9 fw-normal">{user.email}</h5>
            <p className="text-500">{user.role}</p>
            <Link
              to={paths.userSettings}
              className="btn btn-orkestra-primary btn-sm px-3"
            >
              <FontAwesomeIcon icon="pencil-alt" className="me-1" />
              {t('userProfileScaffold.banner.editProfile', {
                defaultValue: 'Edit profile'
              })}
            </Link>
            <Link
              to={paths.userSecurity}
              className="btn btn-orkestra-default btn-sm px-3 ms-2"
            >
              <FontAwesomeIcon icon="shield-alt" className="me-1" />
              {t('userProfileScaffold.banner.security', {
                defaultValue: 'Security'
              })}
            </Link>
            <div className="border-dashed border-bottom my-4 d-lg-none" />
          </Col>
          <Col className="ps-2 ps-lg-3">
            {providers.length === 0 ? (
              <small className="text-muted">
                {t('userProfileScaffold.banner.noLinkedAccounts', {
                  defaultValue: 'No linked accounts.'
                })}
              </small>
            ) : (
              providers.map(p => {
                const provider = p.provider as OAuthProvider;
                const logo = PROVIDER_LOGOS[provider];
                return (
                  <Flex
                    alignItems="center"
                    className="mb-2"
                    key={`${provider}-${p.providerId}`}
                  >
                    {logo ? (
                      <img
                        src={logo}
                        alt={PROVIDER_LABELS[provider]}
                        width={30}
                        className="me-2"
                      />
                    ) : (
                      <div
                        className="me-2 rounded-circle bg-secondary text-white d-inline-flex align-items-center justify-content-center"
                        style={{ width: 30, height: 30, fontWeight: 600 }}
                      >
                        {PROVIDER_LABELS[provider][0]}
                      </div>
                    )}
                    <div className="flex-1">
                      <h6 className="mb-0">{PROVIDER_LABELS[provider]}</h6>
                      <small className="text-muted">{p.email}</small>
                    </div>
                  </Flex>
                );
              })
            )}
          </Col>
        </Row>
      </ProfileBanner.Body>
    </ProfileBanner>
  );
};

export default Banner;
