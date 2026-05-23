import { useEffect, useState } from 'react';
import { useNavigate, useSearchParams } from 'react-router-dom';
import { Card, Spinner, Alert } from 'react-bootstrap';
import { useTranslation } from 'react-i18next';
import { useAppDispatch } from '../../store/hooks';
import { baseApi } from '../../store/api/baseApi';
import AuthCardLayout from 'layouts/AuthCardLayout';

const SocialAuthCallback = () => {
  const { t } = useTranslation();
  const [searchParams] = useSearchParams();
  const navigate = useNavigate();
  const dispatch = useAppDispatch();
  const [status, setStatus] = useState<'loading' | 'success' | 'error'>(
    'loading'
  );
  const [error, setError] = useState<string>('');

  useEffect(() => {
    const processCallback = async () => {
      try {
        // Extract authentication data from URL parameters (sent by backend redirect)
        // Note: All authentication now handled via HttpOnly cookies exclusively
        const success = searchParams.get('success');
        const error = searchParams.get('error');
        const provider = searchParams.get('provider');
        const requiresMfa = searchParams.get('requiresMfa');
        const mfaToken = searchParams.get('mfaToken');
        const webauthnAvailable =
          searchParams.get('webauthnAvailable') === 'true';

        if (error) {
          throw new Error(
            `${t('auth.social.callback.oauthErrorPrefix')}: ${error}`
          );
        }

        // OAuth-resolved user is privileged + MFA-enrolled — backend
        // returned a partial response with no tokens. Route to
        // /mfa/verify with the challenge id so the user can complete
        // the second factor; LoginMfaVerify reads challengeId from
        // location.state, matching the password-login MFA path.
        if (requiresMfa === 'true' && mfaToken) {
          navigate('/mfa/verify', {
            replace: true,
            state: { challengeId: mfaToken, webauthnAvailable, provider }
          });
          return;
        }

        if (success !== 'true') {
          throw new Error(t('auth.social.callback.genericFailure'));
        }

        console.log(
          `${
            provider || 'Social'
          } login successful, invalidating auth cache...`,
          {
            provider,
            timestamp: new Date().toISOString()
          }
        );

        // OAuth backend has already set the refresh token cookie
        // Just invalidate auth cache to trigger the auth hook to fetch session
        // This prevents duplicate session calls while ensuring auth state updates
        dispatch(baseApi.util.invalidateTags(['Auth']));

        // Small delay to let the auth hook process the new session
        await new Promise(resolve => setTimeout(resolve, 100));

        setStatus('success');

        // Clear URL parameters for security (remove tokens from browser history)
        setTimeout(() => {
          window.history.replaceState(
            {},
            document.title,
            window.location.pathname
          );
          navigate('/dashboard/analytics');
        }, 1500);
      } catch (err) {
        console.error('Social OAuth callback error:', err);
        setError(
          err instanceof Error
            ? err.message
            : t('auth.social.callback.genericFailure')
        );
        setStatus('error');

        // Redirect to login page after error
        setTimeout(() => {
          navigate('/login');
        }, 3000);
      }
    };

    processCallback();
  }, [searchParams, navigate, dispatch, t]);

  return (
    <AuthCardLayout>
      <Card>
        <Card.Body className="p-4 p-sm-5 text-center">
          {status === 'loading' && (
            <>
              <Spinner animation="border" variant="primary" className="mb-3" />
              <h5>{t('auth.social.callback.completing')}</h5>
              <p className="text-muted">
                {t('auth.social.callback.waitMessage')}
              </p>
            </>
          )}

          {status === 'success' && (
            <>
              <div className="text-success mb-3">
                <i className="fas fa-check-circle fa-3x"></i>
              </div>
              <h5 className="text-success">
                {t('auth.social.callback.successTitle')}
              </h5>
              <p className="text-muted">
                {t('auth.social.callback.successDetail')}
              </p>
            </>
          )}

          {status === 'error' && (
            <>
              <Alert variant="danger" className="mb-3">
                <h6>{t('auth.social.callback.failureTitle')}</h6>
                <p className="mb-0">{error}</p>
              </Alert>
              <p className="text-muted">
                {t('auth.social.callback.redirectingToLogin')}
              </p>
            </>
          )}
        </Card.Body>
      </Card>
    </AuthCardLayout>
  );
};

export default SocialAuthCallback;
