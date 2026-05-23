import { useState, FormEvent } from 'react';
import { Alert, Button, Form } from 'react-bootstrap';
import { Link, useNavigate } from 'react-router-dom';
import { useTranslation } from 'react-i18next';
import { useGetAuthPolicyQuery, useRegisterMutation } from 'store/api/authApi';

const RegisterForm = () => {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const [register, { isLoading }] = useRegisterMutation();
  const [fullName, setFullName] = useState('');
  const [email, setEmail] = useState('');
  const [password, setPassword] = useState('');
  const [confirm, setConfirm] = useState('');
  const [accepted, setAccepted] = useState(false);
  const [localError, setLocalError] = useState<string | null>(null);
  // Read the admin-managed signup policy. registrationEnabled drives
  // the kill-switch banner + submit gating; passwordMinLength replaces
  // the hardcoded 10-char check so the form stays in sync with what the
  // backend will actually accept.
  const { data: policy } = useGetAuthPolicyQuery();
  const registrationEnabled = policy?.registrationEnabled ?? true;
  const passwordMinLength = policy?.passwordMinLength ?? 10;

  const handleSubmit = async (event: FormEvent) => {
    event.preventDefault();
    setLocalError(null);

    if (!fullName || !email || !password) {
      setLocalError(t('auth.errors.fillRequiredFields'));
      return;
    }
    if (password.length < passwordMinLength) {
      setLocalError(
        t('auth.errors.passwordTooShort', { count: passwordMinLength })
      );
      return;
    }
    if (password !== confirm) {
      setLocalError(t('auth.errors.passwordMismatch'));
      return;
    }
    if (!accepted) {
      setLocalError(t('auth.errors.acceptTerms'));
      return;
    }

    try {
      const result = await register({ email, password, fullName }).unwrap();
      if (result.requiresVerification) {
        navigate(`/verify-email?pending=${encodeURIComponent(email)}`);
      } else {
        navigate('/login?registered=1');
      }
    } catch (err: unknown) {
      const anyErr = err as { data?: { detail?: string }; status?: number };
      if (anyErr?.status === 503) {
        setLocalError(t('auth.errors.signupUnavailable'));
      } else {
        setLocalError(
          anyErr?.data?.detail || t('auth.errors.createAccountFailed')
        );
      }
    }
  };

  return (
    <Form onSubmit={handleSubmit}>
      {!registrationEnabled && (
        <Alert variant="warning" className="mb-3">
          {t('auth.registrationDisabled')}
        </Alert>
      )}
      {localError && (
        <Alert
          variant="danger"
          className="mb-3"
          dismissible
          onClose={() => setLocalError(null)}
        >
          {localError}
        </Alert>
      )}

      <Form.Group className="mb-3">
        <Form.Label>{t('auth.fullName')}</Form.Label>
        <Form.Control
          type="text"
          value={fullName}
          onChange={e => setFullName(e.target.value)}
          autoComplete="name"
          required
        />
      </Form.Group>

      <Form.Group className="mb-3">
        <Form.Label>{t('auth.email')}</Form.Label>
        <Form.Control
          type="email"
          value={email}
          onChange={e => setEmail(e.target.value)}
          autoComplete="email"
          required
        />
      </Form.Group>

      <Form.Group className="mb-3">
        <Form.Label>{t('auth.password')}</Form.Label>
        <Form.Control
          type="password"
          value={password}
          onChange={e => setPassword(e.target.value)}
          autoComplete="new-password"
          minLength={passwordMinLength}
          required
        />
        <Form.Text className="text-muted">
          {t('auth.passwordMinHint', { count: passwordMinLength })}
        </Form.Text>
      </Form.Group>

      <Form.Group className="mb-3">
        <Form.Label>{t('auth.confirmPassword')}</Form.Label>
        <Form.Control
          type="password"
          value={confirm}
          onChange={e => setConfirm(e.target.value)}
          autoComplete="new-password"
          required
        />
      </Form.Group>

      <Form.Group className="mb-3">
        <Form.Check
          type="checkbox"
          id="accept-terms"
          checked={accepted}
          onChange={e => setAccepted(e.target.checked)}
          label={
            <span className="fs--1">
              {t('auth.terms.acceptPrefix')}{' '}
              <Link to="/terms">{t('auth.terms.termsLink')}</Link>{' '}
              {t('auth.terms.and')}{' '}
              <Link to="/privacy">{t('auth.terms.privacyLink')}</Link>
            </span>
          }
        />
      </Form.Group>

      <div className="d-grid mb-3">
        <Button
          type="submit"
          variant="primary"
          size="lg"
          disabled={isLoading || !registrationEnabled}
        >
          {isLoading ? t('auth.registering') : t('auth.createAccount')}
        </Button>
      </div>

      <div className="text-center">
        <small className="text-muted">
          {t('auth.haveAccount')} <Link to="/login">{t('auth.loginHere')}</Link>
        </small>
      </div>
    </Form>
  );
};

export default RegisterForm;
