import { Link } from 'react-router';
import { Badge, Button, Card, Placeholder } from 'react-bootstrap';
import { useTranslation } from 'react-i18next';
import OrkestraCardHeader from 'components/common/OrkestraCardHeader';
import paths from 'routes/paths';
import {
  OAuthProvider,
  useGetCurrentUserQuery,
  useGetMySessionsQuery,
  useGetSelfAuthMethodsQuery
} from 'store/api/authApi';

// SecuritySummaryCard — at-a-glance state of the user's authentication
// posture, surfaced on /user/settings. The full management page lives
// at /user/security (6 tabs) — this card is the read-only summary so
// the settings page surfaces meaningful info instead of a bare link.
//
// Reads three sources:
//   - /v1/auth/operator/me           (oauth providers from BackendUser)
//   - /v1/auth/operator/me/auth-methods (mfa factors, password age)
//   - /v1/auth/operator/me/sessions  (active count)
//
// All three are RTK Query — if /auth-methods 401s on step-up gates,
// the global StepUpModal handles it. We render placeholders during
// load and degrade gracefully on errors (one missing piece doesn't
// hide the others).

const PROVIDER_LABELS: Record<OAuthProvider, string> = {
  google: 'Google',
  apple: 'Apple',
  github: 'GitHub',
  discord: 'Discord'
};

const daysSince = (iso: string | undefined): number | null => {
  if (!iso) return null;
  const ms = Date.now() - new Date(iso).getTime();
  if (!Number.isFinite(ms) || ms < 0) return null;
  return Math.floor(ms / (1000 * 60 * 60 * 24));
};

const SecuritySummaryCard: React.FC = () => {
  const { t } = useTranslation();
  const { data: user } = useGetCurrentUserQuery();
  const { data: authMethods, isLoading: methodsLoading } =
    useGetSelfAuthMethodsQuery();
  const { data: sessionsResp, isLoading: sessionsLoading } =
    useGetMySessionsQuery();

  const isLoading = methodsLoading || sessionsLoading;

  const totpEnrolled = authMethods?.mfaFactors.some(f => f.type === 'totp');
  const passkeyCount =
    authMethods?.mfaFactors.find(f => f.type === 'webauthn')?.credentials
      ?.length ?? 0;
  const hasMfa = !!totpEnrolled || passkeyCount > 0;

  const passwordAgeDays = daysSince(authMethods?.passwordUpdatedAt);
  const sessionCount = sessionsResp?.activeCount ?? 0;
  const linkedProviders = user?.oauthProviders ?? [];

  return (
    <Card className="mb-3">
      <OrkestraCardHeader title={t('settings.security.title')} />
      <Card.Body className="bg-body-tertiary">
        <p className="fs-9 text-muted mb-3">
          {t('settings.security.summary.description')}
        </p>

        {isLoading ? (
          <Placeholder as="ul" animation="glow" className="list-unstyled mb-3">
            {[0, 1, 2, 3].map(i => (
              <li key={i} className="mb-2">
                <Placeholder xs={10} bg="secondary" />
              </li>
            ))}
          </Placeholder>
        ) : (
          <ul className="list-unstyled mb-3 fs-9">
            <li className="mb-2 d-flex align-items-start gap-2">
              <span
                className={`badge rounded-pill mt-1 ${
                  hasMfa ? 'bg-success' : 'bg-warning'
                }`}
                style={{ width: 8, height: 8, padding: 0 }}
                aria-hidden
              />
              <span>
                {hasMfa ? (
                  <>
                    <span className="fw-semibold">
                      {t('settings.security.summary.mfaOn')}
                    </span>
                    <span className="text-muted ms-1">
                      {totpEnrolled && passkeyCount > 0
                        ? t('settings.security.summary.mfaBoth', {
                            count: passkeyCount
                          })
                        : totpEnrolled
                          ? t('settings.security.summary.mfaTotp')
                          : t('settings.security.summary.mfaPasskey', {
                              count: passkeyCount
                            })}
                    </span>
                  </>
                ) : (
                  <>
                    <span className="fw-semibold">
                      {t('settings.security.summary.mfaOff')}
                    </span>
                    {authMethods?.mfaRequired && (
                      <Badge bg="danger" className="ms-2 fs-11">
                        {t('settings.security.summary.mfaRequired')}
                      </Badge>
                    )}
                  </>
                )}
              </span>
            </li>

            <li className="mb-2 d-flex align-items-start gap-2">
              <span
                className="badge rounded-pill bg-info mt-1"
                style={{ width: 8, height: 8, padding: 0 }}
                aria-hidden
              />
              <span>
                <span className="fw-semibold">
                  {t('settings.security.summary.sessions', {
                    count: sessionCount
                  })}
                </span>
              </span>
            </li>

            {authMethods?.hasUsablePassword && (
              <li className="mb-2 d-flex align-items-start gap-2">
                <span
                  className="badge rounded-pill bg-secondary mt-1"
                  style={{ width: 8, height: 8, padding: 0 }}
                  aria-hidden
                />
                <span>
                  <span className="fw-semibold">
                    {passwordAgeDays !== null
                      ? t('settings.security.summary.passwordAgeKnown', {
                          count: passwordAgeDays
                        })
                      : t('settings.security.summary.passwordAgeUnknown')}
                  </span>
                </span>
              </li>
            )}

            {linkedProviders.length > 0 && (
              <li className="mb-2 d-flex align-items-start gap-2">
                <span
                  className="badge rounded-pill bg-primary mt-1"
                  style={{ width: 8, height: 8, padding: 0 }}
                  aria-hidden
                />
                <span>
                  <span className="fw-semibold">
                    {t('settings.security.summary.oauthLinked')}
                  </span>
                  <span className="text-muted ms-1">
                    {linkedProviders
                      .map(
                        p =>
                          PROVIDER_LABELS[p.provider as OAuthProvider] ??
                          p.provider
                      )
                      .join(', ')}
                  </span>
                </span>
              </li>
            )}
          </ul>
        )}

        <Button
          as={Link as any}
          to={paths.userSecurity}
          variant="outline-primary"
          size="sm"
          className="w-100"
        >
          {t('settings.security.manage')}
        </Button>
      </Card.Body>
    </Card>
  );
};

export default SecuritySummaryCard;
