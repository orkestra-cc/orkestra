import { useState } from 'react';
import { Alert, Card } from 'react-bootstrap';
import { useSearchParams } from 'react-router';
import { useTranslation } from 'react-i18next';
import AuthCardLayout from 'layouts/AuthCardLayout';
import EmailPasswordForm from 'components/authentication/EmailPasswordForm';
import SocialLoginForm from 'components/authentication/SocialLoginForm';
import { passwordUiVisible, useGetAuthPolicyQuery } from 'store/api/authApi';

const Login = () => {
  const { t } = useTranslation();
  const [searchParams] = useSearchParams();
  const registered = searchParams.get('registered');
  const reset = searchParams.get('reset');

  const { data: policy } = useGetAuthPolicyQuery();
  const [providerCount, setProviderCount] = useState<number | null>(null);
  const breakGlass = policy?.passwordLoginBreakGlassEffective ?? false;
  // Whether EmailPasswordForm renders anything at all: the ordinary form on
  // a persisted true, or the labelled emergency form under break-glass. It
  // is true while /policy is still in flight (fail-open display), so the
  // page never flickers its layout on a slow fetch.
  const passwordFormVisible = passwordUiVisible(policy) || breakGlass;
  // The alert renders only when BOTH methods are conclusively absent: the
  // password UI is policy-hidden (no break-glass) AND the provider query
  // RESOLVED empty. A provider-query error keeps SocialLoginForm's own
  // retryable alert instead — an outage is not "no method" (§4.10).
  const noMethod =
    policy !== undefined && !passwordFormVisible && providerCount === 0;

  return (
    <AuthCardLayout>
      <Card>
        <Card.Body className="p-4 p-sm-5">
          <div className="text-center mb-4">
            <h3 className="mb-2">{t('auth.pages.loginTitle')}</h3>
            <p className="text-muted mb-0">{t('auth.pages.loginSubtitle')}</p>
          </div>

          {registered && (
            <div className="alert alert-success">
              {t('auth.pages.loginRegisteredFlash')}
            </div>
          )}
          {reset && (
            <div className="alert alert-success">
              {t('auth.pages.loginResetFlash')}
            </div>
          )}

          {noMethod && (
            <Alert variant="warning">{t('auth.pages.loginNoMethod')}</Alert>
          )}

          <EmailPasswordForm />

          {/* Renders its own "or continue with" divider, and nothing at all
              when no provider is enabled. Reports its resolved provider
              count so the alert above needs no second query. The divider is
              suppressed on an SSO-only console: with no password form above
              it, it would divide the buttons from nothing. */}
          <SocialLoginForm
            onProvidersResolved={setProviderCount}
            showDivider={passwordFormVisible}
          />
        </Card.Body>
      </Card>
    </AuthCardLayout>
  );
};

export default Login;
