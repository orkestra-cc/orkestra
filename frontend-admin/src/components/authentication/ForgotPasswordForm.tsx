import { useState, FormEvent } from 'react';
import { Alert, Button, Form } from 'react-bootstrap';
import { Link } from 'react-router-dom';
import { useTranslation } from 'react-i18next';
import { useForgotPasswordMutation } from 'store/api/authApi';

const ForgotPasswordForm = () => {
  const { t } = useTranslation();
  const [email, setEmail] = useState('');
  const [submitted, setSubmitted] = useState(false);
  const [forgotPassword, { isLoading }] = useForgotPasswordMutation();

  const handleSubmit = async (event: FormEvent) => {
    event.preventDefault();
    if (!email) return;
    try {
      await forgotPassword({ email }).unwrap();
    } catch {
      // Generic error to prevent enumeration — always show success.
    }
    setSubmitted(true);
  };

  if (submitted) {
    return (
      <>
        <Alert variant="success" className="mb-3">
          {t('auth.forgot.sent')}
        </Alert>
        <div className="text-center">
          <Link to="/login">{t('auth.forgot.back')}</Link>
        </div>
      </>
    );
  }

  return (
    <Form onSubmit={handleSubmit}>
      <p className="text-muted mb-4">{t('auth.forgot.description')}</p>
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

      <div className="d-grid mb-3">
        <Button type="submit" variant="primary" size="lg" disabled={isLoading}>
          {isLoading ? t('auth.forgot.submitting') : t('auth.forgot.submit')}
        </Button>
      </div>

      <div className="text-center">
        <small className="text-muted">
          {t('auth.forgot.rememberedPrompt')}{' '}
          <Link to="/login">{t('auth.loginHere')}</Link>
        </small>
      </div>
    </Form>
  );
};

export default ForgotPasswordForm;
