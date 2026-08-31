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
  // The alert renders only when BOTH methods are conclusively absent: the
  // password UI is policy-hidden (no break-glass) AND the provider query
  // RESOLVED empty. A provider-query error keeps SocialLoginForm's own
  // retryable alert instead — an outage is not "no method" (§4.10).
  const noMethod =
    policy !== undefined &&
    !passwordUiVisible(policy) &&
    !breakGlass &&
    providerCount === 0;

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
              count so the alert above needs no second query. */}
          <SocialLoginForm onProvidersResolved={setProviderCount} />
        </Card.Body>
      </Card>
    </AuthCardLayout>
  );
};

export default Login;
