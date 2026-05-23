import { useEffect, useState } from 'react';
import { Alert, Button, Card, Spinner } from 'react-bootstrap';
import { Link, useSearchParams } from 'react-router-dom';
import { Trans, useTranslation } from 'react-i18next';
import AuthCardLayout from 'layouts/AuthCardLayout';
import {
  useVerifyEmailMutation,
  useResendVerificationMutation
} from 'store/api/authApi';

type Status = 'pending' | 'verifying' | 'success' | 'error';

const VerifyEmailPage = () => {
  const { t } = useTranslation();
  const [searchParams] = useSearchParams();
  const token = searchParams.get('token');
  const pendingEmail = searchParams.get('pending');
  const [status, setStatus] = useState<Status>(token ? 'verifying' : 'pending');
  const [errorMessage, setErrorMessage] = useState<string | null>(null);
  const [verifyEmail] = useVerifyEmailMutation();
  const [resendVerification, { isLoading: resending, isSuccess: resent }] =
    useResendVerificationMutation();

  useEffect(() => {
    const run = async () => {
      if (!token) return;
      try {
        await verifyEmail({ token }).unwrap();
        setStatus('success');
      } catch (err: unknown) {
        const anyErr = err as { data?: { detail?: string } };
        setErrorMessage(anyErr?.data?.detail || t('auth.verify.failed'));
        setStatus('error');
      }
    };
    void run();
  }, [token, verifyEmail, t]);

  const handleResend = async () => {
    if (!pendingEmail) return;
    try {
      await resendVerification({ email: pendingEmail }).unwrap();
    } catch {
      // Fall through — response is always generic.
    }
  };

  return (
    <AuthCardLayout>
      <Card>
        <Card.Body className="p-4 p-sm-5">
          <div className="text-center mb-4">
            <h3 className="mb-3">{t('auth.verify.title')}</h3>
          </div>

          {status === 'pending' && pendingEmail && (
            <>
              <Alert variant="info">
                <Trans
                  i18nKey="auth.verify.pendingPrompt"
                  values={{ email: pendingEmail }}
                  components={{ strong: <strong /> }}
                />
              </Alert>
              <div className="text-center mt-3">
                <Button
                  variant="outline-primary"
                  onClick={handleResend}
                  disabled={resending || resent}
                >
                  {resent
                    ? t('auth.verify.resent')
                    : resending
                      ? t('auth.verify.resending')
                      : t('auth.verify.resend')}
                </Button>
              </div>
            </>
          )}

          {status === 'pending' && !pendingEmail && (
            <Alert variant="warning">{t('auth.verify.noToken')}</Alert>
          )}

          {status === 'verifying' && (
            <div className="text-center">
              <Spinner animation="border" />
              <p className="mt-3 text-muted">{t('auth.verify.verifying')}</p>
            </div>
          )}

          {status === 'success' && (
            <>
              <Alert variant="success">{t('auth.verify.success')}</Alert>
              <div className="d-grid">
                <Link to="/login" className="btn btn-primary btn-lg">
                  {t('auth.verify.goToLogin')}
                </Link>
              </div>
            </>
          )}

          {status === 'error' && (
            <>
              <Alert variant="danger">{errorMessage}</Alert>
              <div className="text-center">
                <Link to="/login">{t('auth.verify.back')}</Link>
              </div>
            </>
          )}
        </Card.Body>
      </Card>
    </AuthCardLayout>
  );
};

export default VerifyEmailPage;
