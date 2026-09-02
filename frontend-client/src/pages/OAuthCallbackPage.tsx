import { useEffect, useRef, useState } from "react";
import { Link, useLocation, useNavigate } from "react-router";
import { useTranslation } from "react-i18next";

import { useAuth } from "@/auth/useAuth";
import { MfaChallenge } from "@/components/MfaChallenge";
import {
  parseOAuthCallback,
  type OAuthCallbackOutcome,
} from "@/lib/oauthCallback";
import { takeOAuthReturnTo } from "@/lib/oauthReturnTo";
import { DEFAULT_POST_LOGIN } from "@/lib/safeNext";

type Phase = "working" | "signedOut" | "unavailable" | "error";

const FAILURE_CLASS =
  "mb-6 rounded-md border border-red-200 bg-red-50 px-4 py-3 text-left text-sm text-red-800";
const LINK_CLASS = "text-sm text-slate-600 underline hover:text-slate-900";

/**
 * Landing page of the backend's OAuth callback redirect — for the client
 * tier, of the relay endpoint on the API host, which has already set the
 * refresh cookie on a success (handlers/oauth_callback_redirect.go; a
 * CLOSED contract parsed by lib/oauthCallback):
 *   ?success=true&provider=<p>                  → adopt the refresh cookie (bootstrapFromRefreshCookie)
 *   ?success=false&error=<allowlisted code>     → mapped copy, never raw text
 *   #requiresMfa=true&mfaToken=<id>&webauthn…   → render MfaChallenge locally
 *
 * The URL is parsed ONCE on the first render (pure, into a ref) and
 * scrubbed in the first passive effect — before any request — so neither
 * the one-shot challenge id nor the outcome survives in history, a
 * referrer or a reload. The return target is taken-and-deleted in that
 * same effect on every outcome. Success navigates only after the refresh
 * cookie produced an access token: signed-out is a login error; a 503 or
 * a transport failure keeps the page and offers retry (spec §4.10,
 * §5 #23/#27).
 */
export function OAuthCallbackPage() {
  const { t } = useTranslation();
  const location = useLocation();
  const navigate = useNavigate();
  const { signIn, bootstrapFromRefreshCookie } = useAuth();

  // Parsed once, in component memory only. Pure — no storage touched here.
  const outcomeRef = useRef<OAuthCallbackOutcome | null>(null);
  if (outcomeRef.current === null) {
    outcomeRef.current = parseOAuthCallback(location.search, location.hash);
  }
  const outcome = outcomeRef.current;

  // Set by the first effect; null until then (first paint only).
  const [returnTo, setReturnTo] = useState<string | null>(null);
  const [phase, setPhase] = useState<Phase>(
    outcome.kind === "error" ? "error" : "working",
  );
  const [attempt, setAttempt] = useState(0);

  // One-shot, declared before every other effect so it runs first in the
  // commit: take the return target (a destructive read — effect, never
  // render) and replace the history entry with the bare path. A PASSIVE
  // effect on purpose: react-router drops a navigate() issued from a
  // layout effect on initial mount (PR 2 note, spec §0 v4.3). The ref
  // guard covers StrictMode's double invocation.
  const initialised = useRef(false);
  useEffect(() => {
    if (initialised.current) return;
    initialised.current = true;
    setReturnTo(takeOAuthReturnTo() ?? DEFAULT_POST_LOGIN);
    if (location.search || location.hash) {
      navigate(location.pathname, { replace: true });
    }
  }, [location.pathname, location.search, location.hash, navigate]);

  // Success: adopt the cookie, navigate only once it produced a token.
  // Runs after the first effect has set `returnTo` (a later render), so
  // the URL is already clean when the request leaves. `attempt` re-arms
  // it for the retry button. bootstrapFromRefreshCookie never rejects
  // (transport failure is `unavailable`); the catch is a belt for an
  // unexpected throw — it lands on the same retryable phase.
  useEffect(() => {
    if (outcome.kind !== "success" || returnTo === null) return;
    let cancelled = false;
    bootstrapFromRefreshCookie()
      .then((result) => {
        if (cancelled) return;
        if (result.status === "ok") {
          navigate(returnTo, { replace: true });
        } else if (result.status === "signed-out") {
          setPhase("signedOut");
        } else {
          setPhase("unavailable");
        }
      })
      .catch(() => {
        if (!cancelled) setPhase("unavailable");
      });
    return () => {
      cancelled = true;
    };
  }, [attempt, outcome.kind, returnTo, bootstrapFromRefreshCookie, navigate]);

  if (outcome.kind === "mfa") {
    // Never painted before the first effect has scrubbed the URL.
    if (returnTo === null) return null;
    // webauthnAvailable is parsed (closed contract) but the client SPA has
    // no WebAuthn login, so the TOTP / backup-code form renders (D9 — an
    // explicit MVP limitation: a passkey-only user cannot complete here).
    return (
      <section className="mx-auto max-w-md px-6 py-16">
        <h1 className="mb-6 text-3xl font-semibold tracking-tight">
          {t("login.title")}
        </h1>
        <MfaChallenge
          mfaToken={outcome.challengeId}
          onCancel={() => navigate("/login", { replace: true })}
          onSuccess={(result) => {
            signIn(result.accessToken, result.expiresIn);
            navigate(returnTo, { replace: true });
          }}
        />
      </section>
    );
  }

  return (
    <section className="mx-auto max-w-md px-6 py-16 text-center">
      {phase === "working" && (
        <p role="status" aria-busy="true" className="text-sm text-slate-600">
          {t("oauth.callback.verifying")}
        </p>
      )}

      {phase === "error" && outcome.kind === "error" && (
        <>
          <div role="alert" className={FAILURE_CLASS}>
            <p className="font-medium">{t("oauth.callback.failureTitle")}</p>
            <p className="mt-1">
              {t(`oauth.callback.errors.${outcome.errorKey}`)}
            </p>
          </div>
          <Link to="/login" className={LINK_CLASS}>
            {t("oauth.callback.backToLogin")}
          </Link>
        </>
      )}

      {phase === "signedOut" && (
        <>
          <div role="alert" className={FAILURE_CLASS}>
            <p className="font-medium">{t("oauth.callback.failureTitle")}</p>
            <p className="mt-1">{t("oauth.callback.sessionSignedOut")}</p>
          </div>
          <Link to="/login" className={LINK_CLASS}>
            {t("oauth.callback.backToLogin")}
          </Link>
        </>
      )}

      {phase === "unavailable" && (
        <>
          <div
            role="alert"
            className="mb-6 rounded-md border border-amber-200 bg-amber-50 px-4 py-3 text-left text-sm text-amber-900"
          >
            {t("oauth.callback.sessionUnavailable")}
          </div>
          <div className="flex flex-col items-center gap-3">
            <button
              type="button"
              onClick={() => {
                setPhase("working");
                setAttempt((a) => a + 1);
              }}
              className="inline-flex items-center justify-center rounded-md bg-slate-900 px-4 py-2.5 text-sm font-medium text-white hover:bg-slate-700"
            >
              {t("oauth.callback.retry")}
            </button>
            <Link to="/login" className={LINK_CLASS}>
              {t("oauth.callback.backToLogin")}
            </Link>
          </div>
        </>
      )}
    </section>
  );
}
