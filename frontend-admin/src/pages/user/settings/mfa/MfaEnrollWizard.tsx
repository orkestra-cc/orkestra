import { useEffect, useState, FormEvent } from 'react';
import { Alert, Button, Form, Modal, Spinner } from 'react-bootstrap';
import { QRCodeSVG } from 'qrcode.react';
import { useTranslation } from 'react-i18next';
import {
  useEnrollMfaBeginMutation,
  useEnrollMfaConfirmMutation
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
  const { t } = useTranslation();
  const [step, setStep] = useState<Step>('qr');
  const [challengeId, setChallengeId] = useState('');
  const [secret, setSecret] = useState('');
  const [provisioningUri, setProvisioningUri] = useState('');
  const [code, setCode] = useState('');
  const [backupCodes, setBackupCodes] = useState<string[]>([]);
  const [ack, setAck] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const [begin, { isLoading: beginLoading }] = useEnrollMfaBeginMutation();
  const [confirm, { isLoading: confirmLoading }] =
    useEnrollMfaConfirmMutation();

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
      .then(res => {
        setChallengeId(res.challengeId);
        setSecret(res.secret);
        setProvisioningUri(res.provisioningUri);
      })
      .catch((err: { data?: { detail?: string } }) => {
        setError(err?.data?.detail ?? t('userMfa.enrollWizard.beginError'));
      });
  }, [show, begin, t]);

  const handleConfirm = async (event: FormEvent) => {
    event.preventDefault();
    setError(null);
    if (!code.trim()) {
      setError(t('userMfa.enrollWizard.confirmEmptyError'));
      return;
    }
    try {
      const res = await confirm({ challengeId, code: code.trim() }).unwrap();
      setBackupCodes(res.backupCodes ?? []);
      setStep('backup');
    } catch (err: unknown) {
      const anyErr = err as { status?: number; data?: { detail?: string } };
      if (anyErr?.status === 401 || anyErr?.status === 400) {
        setError(t('userMfa.enrollWizard.confirmIncorrectError'));
      } else {
        setError(
          anyErr?.data?.detail ?? t('userMfa.enrollWizard.confirmGenericError')
        );
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
        <Modal.Title>{t('userMfa.enrollWizard.modalTitle')}</Modal.Title>
      </Modal.Header>
      <Modal.Body>
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

        {step === 'qr' && (
          <>
            {beginLoading && (
              <div className="text-center py-3">
                <Spinner size="sm" />
              </div>
            )}
            {provisioningUri && (
              <>
                <p className="fs-10 mb-2">
                  {t('userMfa.enrollWizard.qrIntro')}
                </p>
                <div className="d-flex justify-content-center my-3">
                  <div className="p-3 bg-white rounded border">
                    <QRCodeSVG value={provisioningUri} size={192} level="M" />
                  </div>
                </div>
                <p className="fs-10 text-muted mb-1">
                  {t('userMfa.enrollWizard.qrManualHint')}
                </p>
                <code className="d-block bg-body-tertiary p-2 rounded small text-break">
                  {secret}
                </code>
                <div className="d-flex justify-content-end mt-3">
                  <Button
                    variant="primary"
                    onClick={() => {
                      setStep('confirm');
                      setError(null);
                    }}
                  >
                    {t('userMfa.enrollWizard.qrContinue')}
                  </Button>
                </div>
              </>
            )}
          </>
        )}

        {step === 'confirm' && (
          <Form onSubmit={handleConfirm} noValidate>
            <p className="fs-10 mb-3">
              {t('userMfa.enrollWizard.confirmIntro')}
            </p>
            <Form.Group className="mb-3">
              <Form.Label>{t('userMfa.enrollWizard.codeLabel')}</Form.Label>
              <Form.Control
                type="text"
                inputMode="numeric"
                autoComplete="one-time-code"
                autoFocus
                value={code}
                onChange={e => setCode(e.target.value)}
                placeholder={t('userMfa.enrollWizard.codePlaceholder')}
                required
              />
            </Form.Group>
            <div className="d-flex justify-content-between">
              <Button variant="outline-secondary" onClick={() => setStep('qr')}>
                {t('userMfa.enrollWizard.back')}
              </Button>
              <Button type="submit" variant="primary" disabled={confirmLoading}>
                {confirmLoading
                  ? t('userMfa.enrollWizard.verifying')
                  : t('userMfa.enrollWizard.confirmConfirmButton')}
              </Button>
            </div>
          </Form>
        )}

        {step === 'backup' && (
          <>
            <Alert variant="warning" className="mb-3">
              <strong>{t('userMfa.enrollWizard.backupHeading')}</strong>{' '}
              {t('userMfa.enrollWizard.backupBody')}
            </Alert>
            <div className="bg-body-tertiary p-3 rounded font-monospace mb-3">
              <div className="row g-2">
                {backupCodes.map(c => (
                  <div key={c} className="col-6 text-center">
                    {c}
                  </div>
                ))}
              </div>
            </div>
            <div className="d-flex justify-content-between mb-3">
              <Button variant="outline-secondary" size="sm" onClick={copyCodes}>
                {t('userMfa.enrollWizard.backupCopyButton')}
              </Button>
              <Button
                variant="outline-secondary"
                size="sm"
                onClick={() => {
                  const blob = new Blob([backupCodes.join('\n')], {
                    type: 'text/plain'
                  });
                  const url = URL.createObjectURL(blob);
                  const a = document.createElement('a');
                  a.href = url;
                  a.download = 'orkestra-backup-codes.txt';
                  a.click();
                  URL.revokeObjectURL(url);
                }}
              >
                {t('userMfa.enrollWizard.backupDownloadButton')}
              </Button>
            </div>
            <Form.Check
              type="checkbox"
              id="mfa-backup-ack"
              label={t('userMfa.enrollWizard.backupAckLabel')}
              checked={ack}
              onChange={e => setAck(e.target.checked)}
            />
            <div className="d-flex justify-content-end mt-3">
              <Button variant="primary" disabled={!ack} onClick={onHide}>
                {t('userMfa.enrollWizard.done')}
              </Button>
            </div>
          </>
        )}
      </Modal.Body>
    </Modal>
  );
};

export default MfaEnrollWizard;
