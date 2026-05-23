import { useEffect, useState, FormEvent } from 'react';
import { Alert, Button, Form, Modal } from 'react-bootstrap';
import { useTranslation } from 'react-i18next';
import { useConfirmPasswordMutation } from 'store/api/authApi';
import {
  subscribePasswordConfirm,
  completePasswordConfirm
} from 'store/passwordConfirm';

/**
 * Global password-reconfirm modal. Opens whenever the RTK Query base
 * query calls requestPasswordConfirm() in response to a 401 with
 * code="password_confirm_required" — the backend emits that envelope
 * when the user has no MFA factor enrolled and the policy doesn't
 * require them to enroll, so the step-up gate falls back to a fresh
 * password reconfirm instead of asking for an MFA code that can't exist.
 *
 * Verification calls /v1/auth/operator/me/password-confirm; the mutation's
 * onQueryStarted dispatches a fresh access token (amr += "reauth",
 * last_otp_at = now) into Redux so the paused destructive request
 * replays with the stepped-up bearer.
 */
const PasswordConfirmModal = () => {
  const { t } = useTranslation();
  const [open, setOpen] = useState(false);
  const [password, setPassword] = useState('');
  const [error, setError] = useState<string | null>(null);
  const [confirm, { isLoading }] = useConfirmPasswordMutation();

  useEffect(() => {
    return subscribePasswordConfirm(next => {
      setOpen(next);
      if (!next) {
        setPassword('');
        setError(null);
      }
    });
  }, []);

  const handleSubmit = async (event: FormEvent) => {
    event.preventDefault();
    setError(null);
    if (!password) {
      setError(t('auth.passwordConfirm.errors.missing'));
      return;
    }
    try {
      await confirm({ password }).unwrap();
      // Fresh access token is already in Redux thanks to the mutation's
      // onQueryStarted — signal the paused requests to replay.
      completePasswordConfirm(true);
    } catch (err: unknown) {
      const anyErr = err as {
        status?: number;
        data?: { detail?: string; code?: string };
      };
      if (anyErr?.status === 401) {
        setError(t('auth.passwordConfirm.errors.incorrect'));
      } else if (anyErr?.data?.code === 'password_confirm_unavailable') {
        setError(t('auth.passwordConfirm.errors.unavailable'));
      } else if (anyErr?.status === 429) {
        setError(t('auth.passwordConfirm.errors.tooMany'));
      } else {
        setError(
          anyErr?.data?.detail ?? t('auth.passwordConfirm.errors.generic')
        );
      }
    }
  };

  const handleCancel = () => {
    if (isLoading) return;
    completePasswordConfirm(false);
  };

  return (
    <Modal show={open} onHide={handleCancel} backdrop="static" centered>
      <Modal.Header closeButton={!isLoading}>
        <Modal.Title>{t('auth.passwordConfirm.title')}</Modal.Title>
      </Modal.Header>
      <Form onSubmit={handleSubmit} noValidate>
        <Modal.Body>
          <p className="fs-10 mb-3">{t('auth.passwordConfirm.intro')}</p>

          {error && (
            <Alert
              variant="danger"
              className="mb-3"
              onClose={() => setError(null)}
              dismissible
            >
              {error}
            </Alert>
          )}

          <Form.Group className="mb-2">
            <Form.Label>{t('auth.passwordConfirm.field')}</Form.Label>
            <Form.Control
              type="password"
              autoComplete="current-password"
              autoFocus
              value={password}
              onChange={e => setPassword(e.target.value)}
              required
            />
          </Form.Group>
        </Modal.Body>
        <Modal.Footer>
          <Button
            variant="outline-secondary"
            onClick={handleCancel}
            disabled={isLoading}
          >
            {t('auth.passwordConfirm.cancel')}
          </Button>
          <Button type="submit" variant="primary" disabled={isLoading}>
            {isLoading
              ? t('auth.passwordConfirm.submitting')
              : t('auth.passwordConfirm.submit')}
          </Button>
        </Modal.Footer>
      </Form>
    </Modal>
  );
};

export default PasswordConfirmModal;
