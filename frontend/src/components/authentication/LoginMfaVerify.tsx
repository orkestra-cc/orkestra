import { useEffect, useState, FormEvent } from 'react';
import { Alert, Button, Card, Form } from 'react-bootstrap';
import { Link, useLocation, useNavigate } from 'react-router-dom';
import AuthCardLayout from 'layouts/AuthCardLayout';
import { useAppDispatch } from 'store/hooks';
import {
  useLoginVerifyMfaMutation,
  useWebAuthnLoginBeginMutation,
  useWebAuthnLoginFinishMutation,
} from 'store/api/mfaApi';
import {
  browserSupportsWebAuthn,
  decodeRequestOptions,
  encodeAssertion,
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
      setLocalError('Enter the code shown in your authenticator app.');
      return;
    }
    if (!state.challengeId) return;

    try {
      const res = await verify({
        challengeId: state.challengeId,
        code: code.trim(),
        useBackup,
      }).unwrap();
      dispatch(loginAction({ userData: res.user }));
      navigate('/dashboard/analytics');
    } catch (err: unknown) {
      const anyErr = err as { status?: number; data?: { detail?: string } };
      if (anyErr?.status === 401) {
        setLocalError('Incorrect code. Please try again.');
      } else if (anyErr?.status === 429) {
        setLocalError('Too many attempts. Sign in again from the login page.');
      } else {
        setLocalError(anyErr?.data?.detail ?? 'Unable to verify the code. Please try again.');
      }
    }
  };

  const handlePasskey = async () => {
    setLocalError(null);
    if (!state.challengeId) return;
    setPasskeyBusy(true);
    try {
      const beginRes = await waBegin({ loginChallengeId: state.challengeId }).unwrap();
      const opts = decodeRequestOptions(beginRes.publicKey);
      const cred = (await navigator.credentials.get({ publicKey: opts })) as PublicKeyCredential | null;
      if (!cred) {
        setPasskeyBusy(false);
        return;
      }
      const finishRes = await waFinish({
        loginChallengeId: state.challengeId,
        webauthnChallengeId: beginRes.challengeId,
        assertionResponse: encodeAssertion(cred),
      }).unwrap();
      dispatch(loginAction({ userData: finishRes.user }));
      navigate('/dashboard/analytics');
    } catch (err: unknown) {
      const anyErr = err as { name?: string; status?: number; data?: { detail?: string } };
      if (anyErr?.name === 'NotAllowedError') {
        setLocalError('The passkey prompt was cancelled or timed out.');
      } else if (anyErr?.status === 401) {
        setLocalError('Passkey verification failed. Try again or use your authenticator app.');
      } else {
        setLocalError(anyErr?.data?.detail ?? 'Could not complete passkey sign-in.');
      }
      setPasskeyBusy(false);
    }
  };

  return (
    <AuthCardLayout>
      <Card>
        <Card.Body className="p-4 p-sm-5">
          <div className="text-center mb-4">
            <h3 className="mb-2">Two-factor verification</h3>
            <p className="text-muted mb-0">
              {state.email
                ? `Enter the code from your authenticator for ${state.email}.`
                : 'Enter the code from your authenticator.'}
            </p>
          </div>

        {localError && (
          <Alert variant="danger" className="mb-3" onClose={() => setLocalError(null)} dismissible>
            {localError}
          </Alert>
        )}

        {passkeyOffered && (
          <div className="d-grid mb-3">
            <Button variant="outline-primary" size="lg" disabled={passkeyBusy} onClick={handlePasskey}>
              {passkeyBusy ? 'Waiting for passkey…' : 'Use a passkey'}
            </Button>
            <div className="text-center text-muted fs-10 mt-2">or</div>
          </div>
        )}

        <Form onSubmit={handleSubmit} noValidate>
          <Form.Group className="mb-3">
            <Form.Label>{useBackup ? 'Backup code' : 'Authenticator code'}</Form.Label>
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

          <div className="d-grid mb-3">
            <Button type="submit" variant="primary" size="lg" disabled={isLoading}>
              {isLoading ? 'Verifying…' : 'Verify and sign in'}
            </Button>
          </div>

          <div className="d-flex justify-content-between fs-10">
            <button
              type="button"
              className="btn btn-link p-0"
              onClick={() => { setUseBackup((v) => !v); setCode(''); }}
            >
              {useBackup ? 'Use authenticator app instead' : 'Use a backup code instead'}
            </button>
            <Link to="/login">Back to sign in</Link>
          </div>
        </Form>
        </Card.Body>
      </Card>
    </AuthCardLayout>
  );
};

export default LoginMfaVerify;
