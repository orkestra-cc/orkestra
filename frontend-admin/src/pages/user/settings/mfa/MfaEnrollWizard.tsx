import { useEffect, useState, FormEvent } from 'react';
import { Alert, Button, Form, Modal, Spinner } from 'react-bootstrap';
import { QRCodeSVG } from 'qrcode.react';
import {
  useEnrollMfaBeginMutation,
  useEnrollMfaConfirmMutation,
} from 'store/api/mfaApi';

type Step = 'qr' | 'confirm' | 'backup';

interface Props {
  show: boolean;
  onHide: () => void;
}

/**
 * Three-step TOTP enrollment dialog:
 *   1. QR — render the provisioning URI as a QR + display the raw secret.
 *   2. Confirm — user scans it in their authenticator and types the code back.
 *   3. Backup — display the ten one-shot backup codes EXACTLY ONCE.
 *
 * Steps 1+2 are resumable on error (invalid code). Step 3 is the only
 * opportunity the user ever gets to see the backup codes, so the confirm
 * button is gated behind an acknowledgment checkbox.
 */
const MfaEnrollWizard = ({ show, onHide }: Props) => {
  const [step, setStep] = useState<Step>('qr');
  const [challengeId, setChallengeId] = useState('');
  const [secret, setSecret] = useState('');
  const [provisioningUri, setProvisioningUri] = useState('');
  const [code, setCode] = useState('');
  const [backupCodes, setBackupCodes] = useState<string[]>([]);
  const [ack, setAck] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const [begin, { isLoading: beginLoading }] = useEnrollMfaBeginMutation();
  const [confirm, { isLoading: confirmLoading }] = useEnrollMfaConfirmMutation();

  // Kick off enrollment whenever the modal opens. Re-opening after a close
  // mints a fresh challenge so stale secrets never reach the authenticator.
  useEffect(() => {
    if (!show) return;
    setStep('qr');
    setCode('');
    setBackupCodes([]);
    setAck(false);
    setError(null);

    begin()
      .unwrap()
      .then((res) => {
        setChallengeId(res.challengeId);
        setSecret(res.secret);
        setProvisioningUri(res.provisioningUri);
      })
      .catch((err: { data?: { detail?: string } }) => {
        setError(err?.data?.detail ?? 'Could not start enrollment. Please try again.');
      });
  }, [show, begin]);

  const handleConfirm = async (event: FormEvent) => {
    event.preventDefault();
    setError(null);
    if (!code.trim()) {
      setError('Enter the 6-digit code shown in your authenticator.');
      return;
    }
    try {
      const res = await confirm({ challengeId, code: code.trim() }).unwrap();
      setBackupCodes(res.backupCodes ?? []);
      setStep('backup');
    } catch (err: unknown) {
      const anyErr = err as { status?: number; data?: { detail?: string } };
      if (anyErr?.status === 401 || anyErr?.status === 400) {
        setError('Incorrect code. Double-check the digits and try again.');
      } else {
        setError(anyErr?.data?.detail ?? 'Could not confirm the code. Please try again.');
      }
    }
  };

  const handleClose = () => {
    // Allow closing only after acknowledgment on the backup step so users
    // can't accidentally dismiss and lose their codes.
    if (step === 'backup' && !ack) return;
    onHide();
  };

  const copyCodes = async () => {
    try {
      await navigator.clipboard.writeText(backupCodes.join('\n'));
    } catch {
      // Clipboard API unavailable — the user can still select + copy manually.
    }
  };

  return (
    <Modal show={show} onHide={handleClose} backdrop="static" centered>
      <Modal.Header closeButton={step !== 'backup' || ack}>
        <Modal.Title>Set up two-factor authentication</Modal.Title>
      </Modal.Header>
      <Modal.Body>
        {error && (
          <Alert variant="danger" className="mb-3" onClose={() => setError(null)} dismissible>
            {error}
          </Alert>
        )}

        {step === 'qr' && (
          <>
            {beginLoading && (
              <div className="text-center py-3"><Spinner size="sm" /></div>
            )}
            {provisioningUri && (
              <>
                <p className="fs-10 mb-2">
                  Scan this code with your authenticator app (Google Authenticator, Authy, 1Password…).
                </p>
                <div className="d-flex justify-content-center my-3">
                  <div className="p-3 bg-white rounded border">
                    <QRCodeSVG value={provisioningUri} size={192} level="M" />
                  </div>
                </div>
                <p className="fs-10 text-muted mb-1">Can&apos;t scan? Enter this secret manually:</p>
                <code className="d-block bg-body-tertiary p-2 rounded small text-break">
                  {secret}
                </code>
                <div className="d-flex justify-content-end mt-3">
                  <Button variant="primary" onClick={() => { setStep('confirm'); setError(null); }}>
                    I&apos;ve added it — continue
                  </Button>
                </div>
              </>
            )}
          </>
        )}

        {step === 'confirm' && (
          <Form onSubmit={handleConfirm} noValidate>
            <p className="fs-10 mb-3">
              Enter the 6-digit code your authenticator is showing right now.
            </p>
            <Form.Group className="mb-3">
              <Form.Label>Authenticator code</Form.Label>
              <Form.Control
                type="text"
                inputMode="numeric"
                autoComplete="one-time-code"
                autoFocus
                value={code}
                onChange={(e) => setCode(e.target.value)}
                placeholder="123 456"
                required
              />
            </Form.Group>
            <div className="d-flex justify-content-between">
              <Button variant="outline-secondary" onClick={() => setStep('qr')}>
                Back
              </Button>
              <Button type="submit" variant="primary" disabled={confirmLoading}>
                {confirmLoading ? 'Verifying…' : 'Verify and enable'}
              </Button>
            </div>
          </Form>
        )}

        {step === 'backup' && (
          <>
            <Alert variant="warning" className="mb-3">
              <strong>Save these backup codes now.</strong> They are the only way to sign in if you
              lose access to your authenticator. Each code works once.
            </Alert>
            <div className="bg-body-tertiary p-3 rounded font-monospace mb-3">
              <div className="row g-2">
                {backupCodes.map((c) => (
                  <div key={c} className="col-6 text-center">{c}</div>
                ))}
              </div>
            </div>
            <div className="d-flex justify-content-between mb-3">
              <Button variant="outline-secondary" size="sm" onClick={copyCodes}>
                Copy codes
              </Button>
              <Button
                variant="outline-secondary"
                size="sm"
                onClick={() => {
                  const blob = new Blob([backupCodes.join('\n')], { type: 'text/plain' });
                  const url = URL.createObjectURL(blob);
                  const a = document.createElement('a');
                  a.href = url;
                  a.download = 'orkestra-backup-codes.txt';
                  a.click();
                  URL.revokeObjectURL(url);
                }}
              >
                Download
              </Button>
            </div>
            <Form.Check
              type="checkbox"
              id="mfa-backup-ack"
              label="I have saved these backup codes somewhere safe."
              checked={ack}
              onChange={(e) => setAck(e.target.checked)}
            />
            <div className="d-flex justify-content-end mt-3">
              <Button variant="primary" disabled={!ack} onClick={onHide}>
                Done
              </Button>
            </div>
          </>
        )}
      </Modal.Body>
    </Modal>
  );
};

export default MfaEnrollWizard;
