import { useEffect, useState, FormEvent } from 'react';
import { Alert, Button, Card, Form } from 'react-bootstrap';
import { Link, useLocation, useNavigate } from 'react-router-dom';
import { useTranslation } from 'react-i18next';
import AuthCardLayout from 'layouts/AuthCardLayout';
import { useAppDispatch } from 'store/hooks';
import {
  useLoginVerifyMfaMutation,
  useWebAuthnLoginBeginMutation,
  useWebAuthnLoginFinishMutation
} from 'store/api/mfaApi';
import {
  browserSupportsWebAuthn,
  decodeRequestOptions,
  encodeAssertion
} from 'store/api/webauthnCodec';
import { login as loginAction } from 'store/slices/authSlice';

interface LocationState {
  challengeId?: string;
  email?: string;
  webauthnAvailable?: boolean;
}

/**
 * Completes a login that paused on the MFA challenge. The caller arrives
 * here from EmailPasswordForm with `state.challengeId` set; we either:
 *   - POST a TOTP / backup code to /v1/auth/operator/mfa/login/verify, or
 *   - run the WebAuthn assertion ceremony when state.webauthnAvailable
 *     and the user picks "Use a passkey".
 *
 * Both branches dispatch loginAction with the same BackendUser shape so
 * downstream consumers don't care which factor satisfied the partial.
 */
const LoginMfaVerify = () => {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const dispatch = useAppDispatch();
  const location = useLocation();
  const state = (location.state ?? {}) as LocationState;
  const passkeyOffered = !!state.webauthnAvailable && browserSupportsWebAuthn();

  const [code, setCode] = useState('');
  const [useBackup, setUseBackup] = useState(false);
  const [localError, setLocalError] = useState<string | null>(null);
  const [passkeyBusy, setPasskeyBusy] = useState(false);

  const [verify, { isLoading }] = useLoginVerifyMfaMutation();
  const [waBegin] = useWebAuthnLoginBeginMutation();
  const [waFinish] = useWebAuthnLoginFinishMutation();

  // Without a challenge id we cannot complete the flow — bounce back.
  useEffect(() => {
    if (!state.challengeId) {
      navigate('/login', { replace: true });
    }
  }, [state.challengeId, navigate]);

  const handleSubmit = async (event: FormEvent) => {
    event.preventDefault();
    setLocalError(null);
    if (!code.trim()) {
      setLocalError(t('auth.mfa.errors.missingCode'));
      return;
    }
    if (!state.challengeId) return;

    try {
      const res = await verify({
        challengeId: state.challengeId,
        code: code.trim(),
        useBackup
      }).unwrap();
      dispatch(loginAction({ userData: res.user }));
      navigate('/dashboard/analytics');
    } catch (err: unknown) {
      const anyErr = err as { status?: number; data?: { detail?: string } };
      if (anyErr?.status === 401) {
        setLocalError(t('auth.mfa.errors.incorrectCode'));
      } else if (anyErr?.status === 429) {
        setLocalError(t('auth.mfa.errors.tooMany'));
      } else {
        setLocalError(anyErr?.data?.detail ?? t('auth.mfa.errors.generic'));
      }
    }
  };

  const handlePasskey = async () => {
    setLocalError(null);
    if (!state.challengeId) return;
    setPasskeyBusy(true);
    try {
      const beginRes = await waBegin({
        loginChallengeId: state.challengeId
      }).unwrap();
      const opts = decodeRequestOptions(beginRes.publicKey);
      const cred = (await navigator.credentials.get({
        publicKey: opts
      })) as PublicKeyCredential | null;
      if (!cred) {
        setPasskeyBusy(false);
        return;
      }
      const finishRes = await waFinish({
        loginChallengeId: state.challengeId,
        webauthnChallengeId: beginRes.challengeId,
        assertionResponse: encodeAssertion(cred)
      }).unwrap();
      dispatch(loginAction({ userData: finishRes.user }));
      navigate('/dashboard/analytics');
    } catch (err: unknown) {
      const anyErr = err as {
        name?: string;
        status?: number;
        data?: { detail?: string };
      };
      if (anyErr?.name === 'NotAllowedError') {
        setLocalError(t('auth.mfa.errors.passkeyCancelled'));
      } else if (anyErr?.status === 401) {
        setLocalError(t('auth.mfa.errors.passkeyFailed'));
      } else {
        setLocalError(
          anyErr?.data?.detail ?? t('auth.mfa.errors.passkeyGeneric')
        );
      }
      setPasskeyBusy(false);
    }
  };

  return (
    <AuthCardLayout>
      <Card>
        <Card.Body className="p-4 p-sm-5">
          <div className="text-center mb-4">
            <h3 className="mb-2">{t('auth.mfa.title')}</h3>
            <p className="text-muted mb-0">
              {state.email
                ? t('auth.mfa.promptForEmail', { email: state.email })
                : t('auth.mfa.promptDefault')}
            </p>
          </div>

          {localError && (
            <Alert
              variant="danger"
              className="mb-3"
              onClose={() => setLocalError(null)}
              dismissible
            >
              {localError}
            </Alert>
          )}

          {passkeyOffered && (
            <div className="d-grid mb-3">
              <Button
                variant="outline-primary"
                size="lg"
                disabled={passkeyBusy}
                onClick={handlePasskey}
              >
                {passkeyBusy
                  ? t('auth.mfa.passkeyWaiting')
                  : t('auth.mfa.passkeyButton')}
              </Button>
              <div className="text-center text-muted fs-10 mt-2">
                {t('auth.mfa.passkeyOr')}
              </div>
            </div>
          )}

          <Form onSubmit={handleSubmit} noValidate>
            <Form.Group className="mb-3">
              <Form.Label>
                {useBackup
                  ? t('auth.mfa.backupCode')
                  : t('auth.mfa.authenticatorCode')}
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
                    ? t('auth.mfa.backupPlaceholder')
                    : t('auth.mfa.authenticatorPlaceholder')
                }
                required
              />
            </Form.Group>

            <div className="d-grid mb-3">
              <Button
                type="submit"
                variant="primary"
                size="lg"
                disabled={isLoading}
              >
                {isLoading ? t('auth.mfa.submitting') : t('auth.mfa.submit')}
              </Button>
            </div>

            <div className="d-flex justify-content-between fs-10">
              <button
                type="button"
                className="btn btn-link p-0"
                onClick={() => {
                  setUseBackup(v => !v);
                  setCode('');
                }}
              >
                {useBackup
                  ? t('auth.mfa.useAuthenticator')
                  : t('auth.mfa.useBackup')}
              </button>
              <Link to="/login">{t('auth.mfa.back')}</Link>
            </div>
          </Form>
        </Card.Body>
      </Card>
    </AuthCardLayout>
  );
};

export default LoginMfaVerify;
