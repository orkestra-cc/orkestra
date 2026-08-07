import { Card } from 'react-bootstrap';
import { useSearchParams } from 'react-router';
import { useTranslation } from 'react-i18next';
import AuthCardLayout from 'layouts/AuthCardLayout';
import EmailPasswordForm from 'components/authentication/EmailPasswordForm';
import SocialLoginForm from 'components/authentication/SocialLoginForm';

const Login = () => {
  const { t } = useTranslation();
  const [searchParams] = useSearchParams();
  const registered = searchParams.get('registered');
  const reset = searchParams.get('reset');

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

          <EmailPasswordForm />

          {/* Renders its own "or continue with" divider, and nothing at all
              when no provider is enabled. */}
          <SocialLoginForm />
        </Card.Body>
      </Card>
    </AuthCardLayout>
  );
};

export default Login;
