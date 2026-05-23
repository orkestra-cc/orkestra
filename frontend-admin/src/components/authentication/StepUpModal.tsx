import { useEffect, useState, FormEvent } from 'react';
import { Alert, Button, Form, Modal } from 'react-bootstrap';
import { useTranslation } from 'react-i18next';
import {
  useGetMfaStatusQuery,
  useVerifyMfaMutation,
  useWebAuthnVerifyBeginMutation,
  useWebAuthnVerifyFinishMutation
} from 'store/api/mfaApi';
import {
  browserSupportsWebAuthn,
  decodeRequestOptions,
  encodeAssertion
} from 'store/api/webauthnCodec';
import { subscribeStepUp, completeStepUp } from 'store/stepUp';

/**
 * Global step-up verification modal. Opens whenever the RTK Query base
 * query calls requestStepUp() in response to a 401 with
 * code="step_up_required". Offers two paths to satisfy the gate:
 *   - TOTP / backup code via /v1/auth/operator/mfa/verify (always available);
 *   - Passkey via /v1/auth/operator/mfa/webauthn/verify/{begin,finish}, when the
 *     user has at least one credential and the browser supports WebAuthn.
 *
 * Both branches dispatch a stepped-up access token into Redux through
 * RTK Query's onQueryStarted; on success we resolve the paused requests
 * via completeStepUp(true) so they replay with the fresh bearer.
 */
const StepUpModal = () => {
  const { t } = useTranslation();
  const [open, setOpen] = useState(false);
  const [code, setCode] = useState('');
  const [useBackup, setUseBackup] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [passkeyBusy, setPasskeyBusy] = useState(false);

  // Skip the status query until the modal opens — saves a request on
  // every page load for users who never trigger step-up.
  const { data: mfaStatus } = useGetMfaStatusQuery(undefined, { skip: !open });
  const [verify, { isLoading }] = useVerifyMfaMutation();
  const [waBegin] = useWebAuthnVerifyBeginMutation();
  const [waFinish] = useWebAuthnVerifyFinishMutation();

  const passkeyOffered =
    open &&
    (mfaStatus?.webauthnCredentials ?? 0) > 0 &&
    browserSupportsWebAuthn();

  useEffect(() => {
    return subscribeStepUp(next => {
      setOpen(next);
      if (!next) {
        setCode('');
        setUseBackup(false);
        setError(null);
        setPasskeyBusy(false);
      }
    });
  }, []);

  const handleSubmit = async (event: FormEvent) => {
    event.preventDefault();
    setError(null);
    if (!code.trim()) {
      setError(t('auth.stepUp.errors.missingCode'));
      return;
    }
    try {
      await verify({ code: code.trim(), useBackup }).unwrap();
      // Fresh access token is already in Redux thanks to verifyMfa's
      // onQueryStarted — signal the paused requests to replay.
      completeStepUp(true);
    } catch (err: unknown) {
      const anyErr = err as { status?: number; data?: { detail?: string } };
      if (anyErr?.status === 401) {
        setError(t('auth.stepUp.errors.incorrectCode'));
      } else if (anyErr?.status === 429) {
        setError(t('auth.stepUp.errors.tooMany'));
      } else {
        setError(anyErr?.data?.detail ?? t('auth.stepUp.errors.generic'));
      }
    }
  };

  const handlePasskey = async () => {
    setError(null);
    setPasskeyBusy(true);
    try {
      const beginRes = await waBegin().unwrap();
      const opts = decodeRequestOptions(beginRes.publicKey);
      const cred = (await navigator.credentials.get({
        publicKey: opts
      })) as PublicKeyCredential | null;
      if (!cred) {
        setPasskeyBusy(false);
        return;
      }
      await waFinish({
        challengeId: beginRes.challengeId,
        assertionResponse: encodeAssertion(cred)
      }).unwrap();
      completeStepUp(true);
    } catch (err: unknown) {
      const anyErr = err as {
        name?: string;
        status?: number;
        data?: { detail?: string };
      };
      if (anyErr?.name === 'NotAllowedError') {
        setError(t('auth.stepUp.errors.passkeyCancelled'));
      } else if (anyErr?.status === 401) {
        setError(t('auth.stepUp.errors.passkeyFailed'));
      } else {
        setError(
          anyErr?.data?.detail ?? t('auth.stepUp.errors.passkeyGeneric')
        );
      }
      setPasskeyBusy(false);
    }
  };

  const handleCancel = () => {
    if (isLoading || passkeyBusy) return;
    completeStepUp(false);
  };

  return (
    <Modal show={open} onHide={handleCancel} backdrop="static" centered>
      <Modal.Header closeButton={!isLoading && !passkeyBusy}>
        <Modal.Title>{t('auth.stepUp.title')}</Modal.Title>
      </Modal.Header>
      <Form onSubmit={handleSubmit} noValidate>
        <Modal.Body>
          <p className="fs-10 mb-3">{t('auth.stepUp.intro')}</p>

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

          {passkeyOffered && (
            <div className="d-grid mb-3">
              <Button
                type="button"
                variant="outline-primary"
                disabled={passkeyBusy || isLoading}
                onClick={handlePasskey}
              >
                {passkeyBusy
                  ? t('auth.stepUp.passkeyWaiting')
                  : t('auth.stepUp.passkeyButton')}
              </Button>
              <div className="text-center text-muted fs-10 mt-2">
                {t('auth.stepUp.passkeyOrCode')}
              </div>
            </div>
          )}

          <Form.Group className="mb-2">
            <Form.Label>
              {useBackup
                ? t('auth.stepUp.backupCode')
                : t('auth.stepUp.authenticatorCode')}
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
                  ? t('auth.stepUp.backupPlaceholder')
                  : t('auth.stepUp.authenticatorPlaceholder')
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
              ? t('auth.stepUp.useAuthenticator')
              : t('auth.stepUp.useBackup')}
          </button>
        </Modal.Body>
        <Modal.Footer>
          <Button
            variant="outline-secondary"
            onClick={handleCancel}
            disabled={isLoading || passkeyBusy}
          >
            {t('auth.stepUp.cancel')}
          </Button>
          <Button
            type="submit"
            variant="primary"
            disabled={isLoading || passkeyBusy}
          >
            {isLoading ? t('auth.stepUp.submitting') : t('auth.stepUp.submit')}
          </Button>
        </Modal.Footer>
      </Form>
    </Modal>
  );
};

export default StepUpModal;
