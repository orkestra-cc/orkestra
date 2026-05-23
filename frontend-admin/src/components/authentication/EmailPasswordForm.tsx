import { useState, FormEvent } from 'react';
import { Alert, Button, Form } from 'react-bootstrap';
import { Link, useNavigate } from 'react-router-dom';
import { useTranslation } from 'react-i18next';
import { useAppDispatch } from 'store/hooks';
import { useGetAuthPolicyQuery, useLoginMutation } from 'store/api/authApi';
import { login as loginAction } from 'store/slices/authSlice';

const EmailPasswordForm = () => {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const dispatch = useAppDispatch();
  const [email, setEmail] = useState('');
  const [password, setPassword] = useState('');
  const [localError, setLocalError] = useState<string | null>(null);
  const [login, { isLoading }] = useLoginMutation();
  // Surface admin-managed kill switches — hide the signup CTA when
  // self-service registration is off, show a maintenance banner when
  // login itself is paused. Falls open (everything enabled) on error
  // so a degraded /policy fetch doesn't block legitimate users.
  const { data: policy } = useGetAuthPolicyQuery();
  const loginEnabled = policy?.loginEnabled ?? true;
  const registrationEnabled = policy?.registrationEnabled ?? true;

  const handleSubmit = async (event: FormEvent) => {
    event.preventDefault();
    setLocalError(null);

    if (!email || !password) {
      setLocalError(t('auth.errors.missingFields'));
      return;
    }

    try {
      const result = await login({ email, password }).unwrap();

      // Account has an enrolled second factor — hold the credentials flow
      // and send the user to the verify page with the challenge id.
      if (result.requiresMfa && result.mfaToken) {
        navigate('/mfa/verify', {
          state: {
            challengeId: result.mfaToken,
            email,
            webauthnAvailable: result.webauthnAvailable ?? false
          }
        });
        return;
      }

      if (!result.user) {
        setLocalError(t('auth.errors.unableToSignIn'));
        return;
      }
      dispatch(loginAction({ userData: result.user }));

      navigate('/dashboard/analytics');
    } catch (err: unknown) {
      const anyErr = err as { data?: { detail?: string }; status?: number };
      if (anyErr?.status === 401) {
        setLocalError(t('auth.errors.invalidCredentials'));
      } else if (anyErr?.status === 403) {
        setLocalError(
          anyErr?.data?.detail || t('auth.errors.emailNotVerified')
        );
      } else if (anyErr?.status === 429) {
        setLocalError(t('auth.errors.tooManyAttempts'));
      } else {
        setLocalError(anyErr?.data?.detail || t('auth.errors.unableToSignIn'));
      }
    }
  };

  return (
    <Form onSubmit={handleSubmit}>
      {!loginEnabled && (
        <Alert variant="warning" className="mb-3">
          {t('auth.loginDisabled')}
        </Alert>
      )}
      {localError && (
        <Alert
          variant="danger"
          className="mb-3"
          onClose={() => setLocalError(null)}
          dismissible
        >
          {localError}
        </Alert>
      )}

      <Form.Group className="mb-3" controlId="login-email">
        <Form.Label>{t('auth.email')}</Form.Label>
        <Form.Control
          type="email"
          name="email"
          placeholder={t('auth.emailPlaceholder')}
          value={email}
          onChange={e => setEmail(e.target.value)}
          autoComplete="email"
          required
        />
      </Form.Group>

      <Form.Group className="mb-3" controlId="login-password">
        <div className="d-flex justify-content-between">
          <Form.Label>{t('auth.password')}</Form.Label>
          <Link to="/forgot-password" className="fs--1">
            {t('auth.forgotPassword')}
          </Link>
        </div>
        <Form.Control
          type="password"
          name="password"
          placeholder={t('auth.passwordPlaceholder')}
          value={password}
          onChange={e => setPassword(e.target.value)}
          autoComplete="current-password"
          required
        />
      </Form.Group>

      <div className="d-grid mb-3">
        <Button
          type="submit"
          variant="primary"
          size="lg"
          disabled={isLoading || !loginEnabled}
        >
          {isLoading ? t('auth.signingIn') : t('auth.signIn')}
        </Button>
      </div>

      {registrationEnabled && (
        <div className="text-center">
          <small className="text-muted">
            {t('auth.noAccount')}{' '}
            <Link to="/register">{t('auth.createOne')}</Link>
          </small>
        </div>
      )}
    </Form>
  );
};

export default EmailPasswordForm;
