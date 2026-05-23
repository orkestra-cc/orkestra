import { useState, FormEvent } from 'react';
import { Alert, Button, Form } from 'react-bootstrap';
import { Link, useNavigate, useSearchParams } from 'react-router-dom';
import { useTranslation } from 'react-i18next';
import { useResetPasswordMutation } from 'store/api/authApi';

const PASSWORD_MIN_LENGTH = 10;

const ResetPasswordForm = () => {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const [searchParams] = useSearchParams();
  const token = searchParams.get('token') || '';
  const [password, setPassword] = useState('');
  const [confirm, setConfirm] = useState('');
  const [localError, setLocalError] = useState<string | null>(null);
  const [resetPassword, { isLoading, isSuccess }] = useResetPasswordMutation();

  const handleSubmit = async (event: FormEvent) => {
    event.preventDefault();
    setLocalError(null);

    if (!token) {
      setLocalError(t('auth.reset.missingToken'));
      return;
    }
    if (password.length < PASSWORD_MIN_LENGTH) {
      setLocalError(
        t('auth.errors.passwordTooShort', { count: PASSWORD_MIN_LENGTH })
      );
      return;
    }
    if (password !== confirm) {
      setLocalError(t('auth.errors.passwordMismatch'));
      return;
    }

    try {
      await resetPassword({ token, newPassword: password }).unwrap();
      setTimeout(() => navigate('/login?reset=1'), 1500);
    } catch (err: unknown) {
      const anyErr = err as { data?: { detail?: string }; status?: number };
      setLocalError(anyErr?.data?.detail || t('auth.reset.failed'));
    }
  };

  if (isSuccess) {
    return <Alert variant="success">{t('auth.reset.success')}</Alert>;
  }

  return (
    <Form onSubmit={handleSubmit}>
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

      <p className="text-muted mb-4">{t('auth.reset.intro')}</p>

      <Form.Group className="mb-3">
        <Form.Label>{t('auth.reset.newPassword')}</Form.Label>
        <Form.Control
          type="password"
          value={password}
          onChange={e => setPassword(e.target.value)}
          autoComplete="new-password"
          minLength={PASSWORD_MIN_LENGTH}
          required
        />
        <Form.Text className="text-muted">
          {t('auth.passwordMinHint', { count: PASSWORD_MIN_LENGTH })}
        </Form.Text>
      </Form.Group>

      <Form.Group className="mb-3">
        <Form.Label>{t('auth.reset.confirmNewPassword')}</Form.Label>
        <Form.Control
          type="password"
          value={confirm}
          onChange={e => setConfirm(e.target.value)}
          autoComplete="new-password"
          required
        />
      </Form.Group>

      <div className="d-grid mb-3">
        <Button type="submit" variant="primary" size="lg" disabled={isLoading}>
          {isLoading ? t('auth.reset.submitting') : t('auth.reset.submit')}
        </Button>
      </div>

      <div className="text-center">
        <small className="text-muted">
          <Link to="/login">{t('auth.reset.back')}</Link>
        </small>
      </div>
    </Form>
  );
};

export default ResetPasswordForm;
