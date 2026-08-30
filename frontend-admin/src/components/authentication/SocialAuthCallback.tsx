import { useEffect, useRef, useState } from 'react';
import { Link, useLocation, useNavigate } from 'react-router';
import { Alert, Button, Card, Spinner } from 'react-bootstrap';
import { useTranslation } from 'react-i18next';
import { useAppDispatch } from 'store/hooks';
import { authApi } from 'store/api/authApi';
import { setAccessToken, setUserFromApiResponse } from 'store/slices/authSlice';
import AuthCardLayout from 'layouts/AuthCardLayout';
import MfaVerifyPanel from 'components/authentication/MfaVerifyPanel';
import {
  parseOAuthCallback,
  type OAuthCallbackOutcome
} from 'utils/oauthCallbackParams';
import { takeOAuthReturnTo } from 'utils/socialAuthUtils';
import { DEFAULT_POST_LOGIN } from 'utils/returnTo';

type Phase = 'working' | 'signedOut' | 'unavailable' | 'error';

/**
 * Landing page of the backend's OAuth callback redirect
 * (handlers/oauth_callback_redirect.go — a CLOSED contract, parsed by
 * utils/oauthCallbackParams):
 *   ?success=true&provider=<p>                  → bootstrap the session from the refresh cookie
 *   ?success=false&error=<allowlisted code>     → mapped copy, never raw text
 *   #requiresMfa=true&mfaToken=<id>&webauthn…   → render the MFA panel locally
 *
 * The URL is parsed ONCE on the first render (pure) and scrubbed in the
 * first effect — before any await — so neither the one-shot challenge id
 * nor the outcome survives in history, referrers or a reload. The stashed
 * return target is taken-and-deleted in that same effect (a destructive
 * read never runs during render). Success navigates only after
 * GET /v1/auth/session confirmed the refresh-cookie session; a signed-out
 * answer is a login error, an unavailable one keeps the page and offers
 * retry.
 */
const SocialAuthCallback = () => {
  const { t } = useTranslation();
  const location = useLocation();
  const navigate = useNavigate();
  const dispatch = useAppDispatch();

  // Parsed once, in component memory only. Pure — no storage touched here.
  const outcomeRef = useRef<OAuthCallbackOutcome | null>(null);
  if (outcomeRef.current === null) {
    outcomeRef.current = parseOAuthCallback(location.search, location.hash);
  }
  const outcome = outcomeRef.current;

  // Set by the first effect below; null until then (first paint only).
  const [returnTo, setReturnTo] = useState<string | null>(null);
  const [phase, setPhase] = useState<Phase>(
    outcome.kind === 'error' ? 'error' : 'working'
  );
  const [attempt, setAttempt] = useState(0);

  // One-shot, and declared before every other effect so it runs first in the
  // commit: take the return target (destructive read → effect, never render)
  // and replace the history entry with the bare path. It must be a PASSIVE
  // effect, not a layout effect: React commits layout effects child-first, so
  // in a layout effect here the Router's own history subscription (installed
  // by an ancestor's layout effect) is not live yet and the navigate is
  // silently dropped — which is what react-router's own
  // "call navigate() in a React.useEffect()" warning is about.
  const initialised = useRef(false);
  useEffect(() => {
    if (initialised.current) return;
    initialised.current = true;
    setReturnTo(takeOAuthReturnTo() ?? DEFAULT_POST_LOGIN);
    if (location.search || location.hash) {
      navigate(location.pathname, { replace: true });
    }
  }, [location.pathname, location.search, location.hash, navigate]);

  // Success: force a fresh /v1/auth/session and navigate only once it
  // confirms a user. `attempt` re-arms the effect for the retry button.
  useEffect(() => {
    if (outcome.kind !== 'success' || returnTo === null) return;
    let cancelled = false;
    const subscription = dispatch(
      authApi.endpoints.getSession.initiate(undefined, { forceRefetch: true })
    );
    subscription
      .unwrap()
      .then(session => {
        if (cancelled) return;
        if (!session) {
          setPhase('signedOut');
          return;
        }
        // Mirror what useAuth does from the same cache entry, so the guard
        // on the destination sees an authenticated store immediately.
        dispatch(setUserFromApiResponse(session.user));
        if (session.accessToken) {
          dispatch(
            setAccessToken({
              accessToken: session.accessToken,
              expiresIn: session.expiresIn
            })
          );
        }
        navigate(returnTo, { replace: true });
      })
      .catch(() => {
        if (!cancelled) setPhase('unavailable');
      })
      .finally(() => subscription.unsubscribe());
    return () => {
      cancelled = true;
    };
  }, [attempt, outcome.kind, returnTo, dispatch, navigate]);

  // Terminal error states bounce to the login page after a short pause.
  useEffect(() => {
    if (phase !== 'error' && phase !== 'signedOut') return;
    const timer = setTimeout(() => navigate('/login', { replace: true }), 3000);
    return () => clearTimeout(timer);
  }, [phase, navigate]);

  if (outcome.kind === 'mfa') {
    if (returnTo === null) return null; // before the first effect — never painted
    return (
      <MfaVerifyPanel
        challengeId={outcome.challengeId}
        webauthnAvailable={outcome.webauthnAvailable}
        returnTo={returnTo}
      />
    );
  }

  return (
    <AuthCardLayout>
      <Card>
        <Card.Body className="p-4 p-sm-5 text-center">
          {phase === 'working' && (
            <div aria-busy="true">
              <Spinner animation="border" size="sm" className="me-2" />
              <span className="text-muted">
                {t('auth.social.callback.verifying')}
              </span>
            </div>
          )}

          {phase === 'error' && outcome.kind === 'error' && (
            <>
              <Alert variant="danger" className="mb-3">
                <h6>{t('auth.social.callback.failureTitle')}</h6>
                <p className="mb-0">
                  {t(`auth.social.callback.errors.${outcome.errorKey}`)}
                </p>
              </Alert>
              <p className="text-muted">
                {t('auth.social.callback.redirectingToLogin')}
              </p>
            </>
          )}

          {phase === 'signedOut' && (
            <>
              <Alert variant="danger" className="mb-3">
                <h6>{t('auth.social.callback.failureTitle')}</h6>
                <p className="mb-0">
                  {t('auth.social.callback.sessionSignedOut')}
                </p>
              </Alert>
              <p className="text-muted">
                {t('auth.social.callback.redirectingToLogin')}
              </p>
            </>
          )}

          {phase === 'unavailable' && (
            <>
              <Alert variant="warning" className="mb-3">
                <p className="mb-0">
                  {t('auth.social.callback.sessionUnavailable')}
                </p>
              </Alert>
              <div className="d-grid gap-2">
                <Button
                  variant="orkestra-primary"
                  onClick={() => {
                    setPhase('working');
                    setAttempt(a => a + 1);
                  }}
                >
                  {t('auth.social.callback.retry')}
                </Button>
                <Link to="/login" className="fs-10">
                  {t('auth.social.callback.backToLogin')}
                </Link>
              </div>
            </>
          )}
        </Card.Body>
      </Card>
    </AuthCardLayout>
  );
};

export default SocialAuthCallback;
