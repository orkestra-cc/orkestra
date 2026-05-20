import { useState, FormEvent } from 'react';
import { Alert, Button, Form, Modal } from 'react-bootstrap';
import { useTranslation } from 'react-i18next';
import { useRemoveMfaMutation, useVerifyMfaMutation } from 'store/api/mfaApi';

interface Props {
  show: boolean;
  onHide: () => void;
}

/**
 * Two-factor removal confirmation. Server-side the remove endpoint is gated
 * by RequireStepUp(5m), so we always ask the user for a live code and call
 * /mfa/verify first — the fresh access token it mints satisfies the gate,
 * then /remove succeeds. Doing both from the UI keeps the flow linear even
 * when the user has not verified recently.
 */
const MfaRemoveModal = ({ show, onHide }: Props) => {
  const { t } = useTranslation();
  const [code, setCode] = useState('');
  const [useBackup, setUseBackup] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const [verify, { isLoading: verifyLoading }] = useVerifyMfaMutation();
  const [remove, { isLoading: removeLoading }] = useRemoveMfaMutation();
  const busy = verifyLoading || removeLoading;

  const handleSubmit = async (event: FormEvent) => {
    event.preventDefault();
    setError(null);
    if (!code.trim()) {
      setError(t('userMfa.remove.errors.missingCode'));
      return;
    }
    try {
      await verify({ code: code.trim(), useBackup }).unwrap();
      await remove().unwrap();
      setCode('');
      onHide();
    } catch (err: unknown) {
      const anyErr = err as {
        status?: number;
        data?: { detail?: string; code?: string };
      };
      if (anyErr?.status === 401 && anyErr?.data?.code !== 'step_up_required') {
        setError(t('userMfa.remove.errors.incorrectCode'));
      } else {
        setError(anyErr?.data?.detail ?? t('userMfa.remove.errors.generic'));
      }
    }
  };

  const handleClose = () => {
    if (busy) return;
    setCode('');
    setError(null);
    onHide();
  };

  return (
    <Modal show={show} onHide={handleClose} centered>
      <Modal.Header closeButton={!busy}>
        <Modal.Title>{t('userMfa.remove.title')}</Modal.Title>
      </Modal.Header>
      <Form onSubmit={handleSubmit} noValidate>
        <Modal.Body>
          <Alert variant="warning" className="mb-3">
            {t('userMfa.remove.warning')}
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

          <Form.Group className="mb-2">
            <Form.Label>
              {useBackup
                ? t('userMfa.remove.backupCode')
                : t('userMfa.remove.authenticatorCode')}
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
                  ? t('userMfa.remove.backupPlaceholder')
                  : t('userMfa.remove.authenticatorPlaceholder')
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
              ? t('userMfa.remove.useAuthenticator')
              : t('userMfa.remove.useBackup')}
          </button>
        </Modal.Body>
        <Modal.Footer>
          <Button
            variant="outline-secondary"
            onClick={handleClose}
            disabled={busy}
          >
            {t('userMfa.remove.cancel')}
          </Button>
          <Button type="submit" variant="danger" disabled={busy}>
            {busy ? t('userMfa.remove.removing') : t('userMfa.remove.remove')}
          </Button>
        </Modal.Footer>
      </Form>
    </Modal>
  );
};

export default MfaRemoveModal;
