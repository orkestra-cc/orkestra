import { useState, FormEvent } from 'react';
import { Alert, Button, Form, Modal } from 'react-bootstrap';
import { Trans, useTranslation } from 'react-i18next';
import {
  useAdminResetClientUserMfaMutation,
  useAdminResetUserMfaMutation,
  useVerifyMfaMutation
} from 'store/api/mfaApi';
import type { User } from 'store/api/userApi';

interface Props {
  show: boolean;
  user: User | { id: string; email: string; fullName?: string } | null;
  onHide: () => void;
  // Target tier — "operator" hits /v1/admin/users/.../mfa/reset, "client"
  // hits /v1/admin/client-users/.../mfa/reset. Defaults to operator so
  // the existing operator-side users page keeps its behaviour.
  tier?: 'operator' | 'client';
}

/**
 * Admin: wipe a target user's MFA factor so they must re-enroll. The backend
 * route is gated by RequireStepUp(5m), so we always ask the admin for their
 * own live code first — /mfa/verify refreshes the session with a fresh
 * last_otp_at, then the reset call succeeds immediately. One linear flow,
 * no round-trip through the step-up toast.
 */
const AdminResetMfaModal = ({
  show,
  user,
  onHide,
  tier = 'operator'
}: Props) => {
  const { t } = useTranslation();
  const [code, setCode] = useState('');
  const [useBackup, setUseBackup] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const [verify, { isLoading: verifyLoading }] = useVerifyMfaMutation();
  // Both mutation hooks are unconditionally instantiated (rules of hooks);
  // only the one matching `tier` is invoked when the form submits.
  const [resetOperator, { isLoading: resetOperatorLoading }] =
    useAdminResetUserMfaMutation();
  const [resetClient, { isLoading: resetClientLoading }] =
    useAdminResetClientUserMfaMutation();
  const reset = tier === 'client' ? resetClient : resetOperator;
  const resetLoading = resetOperatorLoading || resetClientLoading;
  const busy = verifyLoading || resetLoading;

  const handleSubmit = async (event: FormEvent) => {
    event.preventDefault();
    setError(null);
    if (!user) return;
    if (!code.trim()) {
      setError(t('adminUsers.mfaReset.errors.missingCode'));
      return;
    }
    try {
      await verify({ code: code.trim(), useBackup }).unwrap();
      await reset({ userId: user.id }).unwrap();
      setCode('');
      onHide();
    } catch (err: unknown) {
      const anyErr = err as {
        status?: number;
        data?: { detail?: string; code?: string };
      };
      if (anyErr?.status === 401 && anyErr?.data?.code !== 'step_up_required') {
        setError(t('adminUsers.mfaReset.errors.incorrectCode'));
      } else if (anyErr?.status === 404) {
        setError(t('adminUsers.mfaReset.errors.noFactor'));
      } else {
        setError(
          anyErr?.data?.detail ?? t('adminUsers.mfaReset.errors.generic')
        );
      }
    }
  };

  const handleClose = () => {
    if (busy) return;
    setCode('');
    setError(null);
    onHide();
  };

  if (!user) return null;

  return (
    <Modal show={show} onHide={handleClose} centered>
      <Modal.Header closeButton={!busy}>
        <Modal.Title>
          {t('adminUsers.mfaReset.title', {
            user: user.fullName || user.email
          })}
        </Modal.Title>
      </Modal.Header>
      <Form onSubmit={handleSubmit} noValidate>
        <Modal.Body>
          <Alert variant="warning" className="mb-3">
            {t('adminUsers.mfaReset.warning')}
          </Alert>

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

          <p className="fs-10 mb-3">
            <Trans
              i18nKey="adminUsers.mfaReset.prompt"
              components={{ strong: <strong /> }}
            />
          </p>

          <Form.Group className="mb-2">
            <Form.Label>
              {useBackup
                ? t('adminUsers.mfaReset.yourBackup')
                : t('adminUsers.mfaReset.yourCode')}
            </Form.Label>
            <Form.Control
              type="text"
              inputMode={useBackup ? 'text' : 'numeric'}
              autoComplete="one-time-code"
              autoFocus
              value={code}
              onChange={e => setCode(e.target.value)}
              placeholder={
                useBackup
                  ? t('adminUsers.mfaReset.backupPlaceholder')
                  : t('adminUsers.mfaReset.authenticatorPlaceholder')
              }
              required
            />
          </Form.Group>
          <button
            type="button"
            className="btn btn-link p-0 fs-10"
            onClick={() => {
              setUseBackup(v => !v);
              setCode('');
            }}
          >
            {useBackup
              ? t('adminUsers.mfaReset.useAuthenticator')
              : t('adminUsers.mfaReset.useBackup')}
          </button>
        </Modal.Body>
        <Modal.Footer>
          <Button
            variant="outline-secondary"
            onClick={handleClose}
            disabled={busy}
          >
            {t('adminUsers.mfaReset.cancel')}
          </Button>
          <Button type="submit" variant="warning" disabled={busy}>
            {busy
              ? t('adminUsers.mfaReset.submitting')
              : t('adminUsers.mfaReset.submit')}
          </Button>
        </Modal.Footer>
      </Form>
    </Modal>
  );
};

export default AdminResetMfaModal;
