import { useEffect } from 'react';
import { useLocation, useNavigate } from 'react-router';
import MfaVerifyPanel from 'components/authentication/MfaVerifyPanel';
import { DEFAULT_POST_LOGIN, sanitizeReturnTo } from 'utils/returnTo';

interface LocationState {
  challengeId?: string;
  email?: string;
  webauthnAvailable?: boolean;
  // Deep link captured before login, forwarded by EmailPasswordForm so MFA
  // completion lands on the originally-requested page.
  returnTo?: string;
}

/**
 * Password-login MFA page: the caller (EmailPasswordForm) arrives here with
 * the challenge in `location.state`. The OAuth path does NOT use this page —
 * SocialAuthCallback renders MfaVerifyPanel locally from a scrubbed fragment
 * so the one-shot challenge never sits in router or browser history state.
 */
const LoginMfaVerify = () => {
  const navigate = useNavigate();
  const location = useLocation();
  const state = (location.state ?? {}) as LocationState;
  const returnTo = sanitizeReturnTo(state.returnTo) ?? DEFAULT_POST_LOGIN;

  // Without a challenge id we cannot complete the flow — bounce back.
  useEffect(() => {
    if (!state.challengeId) {
      navigate('/login', { replace: true });
    }
  }, [state.challengeId, navigate]);

  if (!state.challengeId) return null;
  return (
    <MfaVerifyPanel
      challengeId={state.challengeId}
      email={state.email}
      webauthnAvailable={!!state.webauthnAvailable}
      returnTo={returnTo}
    />
  );
};

export default LoginMfaVerify;
