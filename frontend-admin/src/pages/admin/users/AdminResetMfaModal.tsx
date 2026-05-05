import { useState, FormEvent } from 'react';
import { Alert, Button, Form, Modal } from 'react-bootstrap';
import {
  useAdminResetUserMfaMutation,
  useVerifyMfaMutation,
} from 'store/api/mfaApi';
import type { User } from 'store/api/userApi';

interface Props {
  show: boolean;
  user: User | null;
  onHide: () => void;
}

/**
 * Admin: wipe a target user's MFA factor so they must re-enroll. The backend
 * route is gated by RequireStepUp(5m), so we always ask the admin for their
 * own live code first — /mfa/verify refreshes the session with a fresh
 * last_otp_at, then the reset call succeeds immediately. One linear flow,
 * no round-trip through the step-up toast.
 */
const AdminResetMfaModal = ({ show, user, onHide }: Props) => {
  const [code, setCode] = useState('');
  const [useBackup, setUseBackup] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const [verify, { isLoading: verifyLoading }] = useVerifyMfaMutation();
  const [reset, { isLoading: resetLoading }] = useAdminResetUserMfaMutation();
  const busy = verifyLoading || resetLoading;

  const handleSubmit = async (event: FormEvent) => {
    event.preventDefault();
    setError(null);
    if (!user) return;
    if (!code.trim()) {
      setError('Enter your authenticator code to authorize the reset.');
      return;
    }
    try {
      await verify({ code: code.trim(), useBackup }).unwrap();
      await reset({ userId: user.id }).unwrap();
      setCode('');
      onHide();
    } catch (err: unknown) {
      const anyErr = err as { status?: number; data?: { detail?: string; code?: string } };
      if (anyErr?.status === 401 && anyErr?.data?.code !== 'step_up_required') {
        setError('Incorrect code. Please try again.');
      } else if (anyErr?.status === 404) {
        setError('This user has no MFA factor to reset.');
      } else {
        setError(anyErr?.data?.detail ?? 'Could not reset the factor. Please try again.');
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
        <Modal.Title>Reset MFA for {user.fullName || user.email}</Modal.Title>
      </Modal.Header>
      <Form onSubmit={handleSubmit} noValidate>
        <Modal.Body>
          <Alert variant="warning" className="mb-3">
            This will delete the user&apos;s registered authenticator and backup codes. They will be
            forced to enroll a new factor on their next sign-in, subject to the 7-day grace window.
          </Alert>

          {error && (
            <Alert variant="danger" className="mb-3" onClose={() => setError(null)} dismissible>
              {error}
            </Alert>
          )}

          <p className="fs-10 mb-3">
            Enter <strong>your own</strong> authenticator code to authorize this action.
          </p>

          <Form.Group className="mb-2">
            <Form.Label>{useBackup ? 'Your backup code' : 'Your authenticator code'}</Form.Label>
            <Form.Control
              type="text"
              inputMode={useBackup ? 'text' : 'numeric'}
              autoComplete="one-time-code"
              autoFocus
              value={code}
              onChange={(e) => setCode(e.target.value)}
              placeholder={useBackup ? 'XXXX-XXXX' : '123 456'}
              required
            />
          </Form.Group>
          <button
            type="button"
            className="btn btn-link p-0 fs-10"
            onClick={() => { setUseBackup((v) => !v); setCode(''); }}
          >
            {useBackup ? 'Use authenticator app instead' : 'Use a backup code instead'}
          </button>
        </Modal.Body>
        <Modal.Footer>
          <Button variant="outline-secondary" onClick={handleClose} disabled={busy}>
            Cancel
          </Button>
          <Button type="submit" variant="warning" disabled={busy}>
            {busy ? 'Resetting…' : 'Reset MFA'}
          </Button>
        </Modal.Footer>
      </Form>
    </Modal>
  );
};

export default AdminResetMfaModal;
