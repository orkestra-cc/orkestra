import { useEffect, useState, type FormEvent } from "react";
import { Link, Navigate, useNavigate, useSearchParams } from "react-router";
import {
  useMutation,
  useQuery,
  type UseQueryResult,
} from "@tanstack/react-query";
import { useTranslation } from "react-i18next";

import {
  apiErrorCode,
  fetchAuthPolicy,
  fetchOAuthProviders,
  initiateOAuthLogin,
  login,
  passwordLoginUsable,
  type LoginResult,
} from "@/api/auth";
import { resendVerificationEmail } from "@/api/verifyEmail";
import { useAuth } from "@/auth/useAuth";
import { MfaChallenge } from "@/components/MfaChallenge";
import {
  OAUTH_PROVIDER_LABELS,
  type OAuthProviderName,
} from "@/lib/oauthProviders";
import { DEFAULT_POST_LOGIN, sanitizeNext } from "@/lib/safeNext";

// Backend marks the "address not verified" 403 with code="auth.email_not_verified"
// (see auth/handlers/password_handler.go::mapPasswordError). We discriminate
// on the code, not on the localized detail string.
function isEmailNotVerified(err: unknown): boolean {
  return apiErrorCode(err) === "auth.email_not_verified";
}

// Two-state page: credentials (default) → mfa-required (after a partial
// login response carries requiresMfa=true). State lives in the local
// component because a navigation away should drop the in-flight
// challenge — the backend's mfaToken is short-lived and one-shot
// anyway. On full success (either branch) we stamp the in-memory token
// + session marker via AuthProvider.signIn and redirect to the validated
// ?next= or /account.
type Stage =
  | { name: "credentials" }
  | { name: "mfa"; mfaToken: string; webauthnAvailable: boolean };

const INPUT_CLASS =
  "block w-full rounded-md border border-slate-300 bg-white px-3 py-2 text-sm focus:border-slate-500 focus:outline-none focus:ring-1 focus:ring-slate-500";
const PRIMARY_BUTTON_CLASS =
  "inline-flex w-full items-center justify-center rounded-md bg-slate-900 px-4 py-2.5 text-sm font-medium text-white hover:bg-slate-700 disabled:cursor-not-allowed disabled:bg-slate-400";
const NOTICE_CLASS =
  "mb-6 rounded-md border border-amber-200 bg-amber-50 px-4 py-3 text-sm text-amber-900";

export function LoginPage() {
  const { t } = useTranslation();
  const { signIn, isAuthenticated, isBootstrapping } = useAuth();
  const navigate = useNavigate();
  const [params] = useSearchParams();
  // One redirect gate for both sign-in paths (lib/safeNext.ts): the deep
  // link RequireAuth stamped on ?next= is honoured only when it is a
  // same-origin relative path outside the auth routes; otherwise /account.
  // useSearchParams already decoded the value once — no second decode.
  const next = sanitizeNext(params.get("next"));
  const destination = next ?? DEFAULT_POST_LOGIN;

  const [stage, setStage] = useState<Stage>({ name: "credentials" });
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");

  // Public policy. fetchAuthPolicy never rejects (it falls open on any
  // failure — spec §4.10), so `policy === undefined` means exactly "still
  // loading": the page paints neither sign-in surface until it lands
  // (deviation D1) rather than flashing a password form on an SSO-only
  // surface.
  const { data: policy } = useQuery({
    queryKey: ["authPolicy"],
    queryFn: fetchAuthPolicy,
    staleTime: 30_000,
  });
  const loginEnabled = policy?.loginEnabled ?? true;
  const registrationEnabled = policy?.registrationEnabled ?? true;
  const passwordOn = passwordLoginUsable(policy);

  // Providers the backend will accept a login from right now
  // (GET /v1/auth/client/providers). Runs in parallel with the policy
  // read. A rejection — 503, network failure, malformed body — is a
  // retryable error state, never "no method".
  const providers = useQuery({
    queryKey: ["oauthProviders"],
    queryFn: ({ signal }) => fetchOAuthProviders(signal),
    staleTime: 30_000,
  });

  // Takes the whole result rather than `result.accessToken`: the lifetime is
  // already on it, and the two `.accessToken` projections below are exactly
  // where it used to be lost.
  function complete(result: { accessToken: string; expiresIn?: number }) {
    signIn(result.accessToken, result.expiresIn);
    navigate(destination, { replace: true });
  }

  const loginMutation = useMutation<LoginResult, Error, void>({
    mutationFn: () => login({ email: email.trim(), password }),
    onSuccess: (result) => {
      if (result.kind === "mfa_required") {
        setStage({
          name: "mfa",
          mfaToken: result.mfaToken,
          webauthnAvailable: result.webauthnAvailable,
        });
        return;
      }
      complete(result);
    },
  });

  function onSubmitCredentials(e: FormEvent<HTMLFormElement>) {
    e.preventDefault();
    if (!email.trim() || !password) return;
    loginMutation.mutate();
  }

  // Already signed in: a returning visitor who bookmarked /login, or one
  // the guard bounced here before it learned to wait (spec §8 #11). Same
  // destination as complete() computes above — one gate, one fallback, so
  // the cold-load redirect and the post-sign-in one cannot drift apart.
  // `isBootstrapping` keeps this from firing on a half-decided session;
  // by the time complete() runs it is long false, so the sign-in path is
  // unaffected (its own navigate() still fires, to the same place).
  if (isAuthenticated && !isBootstrapping) {
    return <Navigate to={destination} replace />;
  }

  if (stage.name === "mfa") {
    return (
      <section className="mx-auto max-w-md px-6 py-16">
        <h1 className="mb-6 text-3xl font-semibold tracking-tight">
          {t("login.title")}
        </h1>
        <MfaChallenge
          mfaToken={stage.mfaToken}
          onCancel={() => setStage({ name: "credentials" })}
          onSuccess={(result) => complete(result)}
        />
      </section>
    );
  }

  if (policy === undefined) {
    return (
      <section className="mx-auto max-w-md px-6 py-16">
        <h1 className="mb-2 text-3xl font-semibold tracking-tight">
          {t("login.title")}
        </h1>
        <p role="status" className="text-sm text-slate-500">
          {t("loading")}
        </p>
      </section>
    );
  }

  // The no-method alert needs three settled facts: kill switch off,
  // persisted password policy false/null, and a provider list that has
  // RESOLVED empty. A provider-query error keeps its own retryable state.
  const noMethod =
    loginEnabled &&
    !passwordOn &&
    providers.isSuccess &&
    providers.data.length === 0;

  return (
    <section className="mx-auto max-w-md px-6 py-16">
      <h1 className="mb-2 text-3xl font-semibold tracking-tight">
        {t("login.title")}
      </h1>
      <p className="mb-8 text-slate-600">
        {passwordOn ? t("login.subtitle") : t("login.subtitleSso")}
      </p>

      {!loginEnabled && (
        <div className={NOTICE_CLASS} role="alert">
          {t("login.disabled")}
        </div>
      )}

      {noMethod && (
        <div className={NOTICE_CLASS} role="alert">
          {t("login.noMethod")}
        </div>
      )}

      {passwordOn && (
        <form onSubmit={onSubmitCredentials} noValidate className="space-y-5">
          <div>
            <label
              htmlFor="email"
              className="mb-1 block text-sm font-medium text-slate-700"
            >
              {t("login.email")}
            </label>
            <input
              id="email"
              type="email"
              autoComplete="email"
              required
              value={email}
              onChange={(e) => setEmail(e.target.value)}
              className={INPUT_CLASS}
            />
          </div>
          <div>
            <label
              htmlFor="password"
              className="mb-1 block text-sm font-medium text-slate-700"
            >
              {t("login.password")}
            </label>
            <input
              id="password"
              type="password"
              autoComplete="current-password"
              required
              value={password}
              onChange={(e) => setPassword(e.target.value)}
              className={INPUT_CLASS}
            />
          </div>

          {loginMutation.isError &&
            !isEmailNotVerified(loginMutation.error) && (
              <p
                className="rounded-md bg-red-50 px-3 py-2 text-sm text-red-700"
                role="alert"
              >
                {loginMutation.error.message}
              </p>
            )}

          {loginMutation.isError && isEmailNotVerified(loginMutation.error) && (
            <EmailNotVerifiedNotice email={email.trim()} />
          )}

          <button
            type="submit"
            disabled={loginMutation.isPending || !loginEnabled}
            className={PRIMARY_BUTTON_CLASS}
          >
            {loginMutation.isPending
              ? t("login.submitting")
              : t("login.submit")}
          </button>

          <div className="flex items-center justify-between text-sm">
            <Link
              to="/forgot-password"
              className="text-slate-600 underline hover:text-slate-900"
            >
              {t("login.forgot")}
            </Link>
            {registrationEnabled && (
              <Link
                to="/signup"
                className="text-slate-600 underline hover:text-slate-900"
              >
                {t("login.signupLink")}
              </Link>
            )}
          </div>
        </form>
      )}

      {loginEnabled && (
        <OAuthProviderButtons
          providers={providers}
          next={next}
          showDivider={passwordOn}
        />
      )}
    </section>
  );
}

// Inline panel rendered when login returns code="auth.email_not_verified".
// The email field already has a value (we just submitted it), so we
// don't ask the user to retype — one click triggers the resend.
//
// The 60s cooldown is a UX nudge against rapid clicks; the real abuse
// gate is the shared rate limiter on the backend (per-IP + per-email
// buckets, same surface that protects login). The success message is
// neutral by design: the backend always returns 200, so we cannot tell
// the user whether the address was actually known.
interface EmailNotVerifiedNoticeProps {
  email: string;
}

function EmailNotVerifiedNotice({ email }: EmailNotVerifiedNoticeProps) {
  const { t } = useTranslation();
  const [cooldownLeft, setCooldownLeft] = useState(0);

  const resend = useMutation<unknown, Error, string>({
    mutationFn: (addr: string) => resendVerificationEmail(addr),
    onSuccess: () => setCooldownLeft(60),
  });

  useEffect(() => {
    if (cooldownLeft <= 0) return;
    const id = window.setTimeout(() => setCooldownLeft((s) => s - 1), 1000);
    return () => window.clearTimeout(id);
  }, [cooldownLeft]);

  const canSend = !!email && !resend.isPending && cooldownLeft === 0;

  return (
    <div
      className="rounded-md border border-amber-200 bg-amber-50 px-3 py-3 text-sm text-amber-900"
      role="alert"
    >
      <p className="font-medium">{t("login.notVerified.title")}</p>
      <p className="mt-1 text-amber-800">{t("login.notVerified.body")}</p>

      {resend.isSuccess ? (
        <p
          className="mt-3 rounded-md bg-emerald-50 px-3 py-2 text-emerald-700"
          role="status"
        >
          {t("login.notVerified.resendDone")}
        </p>
      ) : (
        <button
          type="button"
          disabled={!canSend}
          onClick={() => email && resend.mutate(email)}
          className="mt-3 inline-flex items-center justify-center rounded-md bg-slate-900 px-3 py-2 text-sm font-medium text-white hover:bg-slate-700 disabled:cursor-not-allowed disabled:bg-slate-400"
        >
          {cooldownLeft > 0
            ? t("login.notVerified.resendCooldown", { seconds: cooldownLeft })
            : resend.isPending
              ? t("login.notVerified.resendSending")
              : t("login.notVerified.resendCta")}
        </button>
      )}
    </div>
  );
}

// startErrorKey maps the backend `code` of a failed OAuth start to copy
// (deviation D6). Anything unmapped is the generic key — the backend
// detail string is never rendered.
function startErrorKey(e: unknown): string {
  switch (apiErrorCode(e)) {
    case "auth.oauth_provider_disabled":
      return "login.oauth.providerDisabled";
    case "auth.policy_unavailable":
      return "error.policyUnavailable";
    case "auth.login_disabled":
      return "login.disabled";
    default:
      return "login.oauth.startFailed";
  }
}

interface OAuthProviderButtonsProps {
  providers: UseQueryResult<OAuthProviderName[], Error>;
  next: string | null;
  // False when no password form renders above: the "or continue with"
  // divider would have nothing to divide from.
  showDivider: boolean;
}

// The provider section of the login page. Three distinct states (spec
// §4.10): loading, a retryable error, and the resolved list — an empty
// list renders nothing here (the page owns the no-method alert). The
// buttons are text-only on purpose: brand names, no icon library.
function OAuthProviderButtons({
  providers,
  next,
  showDivider,
}: OAuthProviderButtonsProps) {
  const { t } = useTranslation();
  const [starting, setStarting] = useState<OAuthProviderName | null>(null);
  const [startError, setStartError] = useState<{
    key: string;
    provider: OAuthProviderName;
  } | null>(null);

  async function start(provider: OAuthProviderName) {
    setStarting(provider);
    setStartError(null);
    try {
      // Stashes the validated `next`, then leaves the SPA — on success
      // there is nothing to reset here.
      await initiateOAuthLogin(provider, next);
    } catch (e) {
      setStartError({ key: startErrorKey(e), provider });
      setStarting(null);
    }
  }

  const divider = showDivider ? (
    <p className="my-6 text-center text-xs uppercase tracking-wide text-slate-400">
      {t("login.oauth.divider")}
    </p>
  ) : null;

  if (providers.isPending) {
    return (
      <>
        {divider}
        <p role="status" className="text-center text-sm text-slate-500">
          {t("login.oauth.loading")}
        </p>
      </>
    );
  }

  if (providers.isError) {
    return (
      <>
        {divider}
        <div
          className="rounded-md border border-amber-200 bg-amber-50 px-4 py-3 text-sm text-amber-900"
          role="alert"
        >
          <p>{t("login.oauth.loadError")}</p>
          <button
            type="button"
            onClick={() => void providers.refetch()}
            className="mt-2 text-sm font-medium underline hover:text-amber-950"
          >
            {t("login.oauth.retry")}
          </button>
        </div>
      </>
    );
  }

  if (providers.data.length === 0) return null;

  return (
    <>
      {divider}
      <div className="space-y-3">
        {startError && (
          <p
            className="rounded-md bg-red-50 px-3 py-2 text-sm text-red-700"
            role="alert"
          >
            {t(startError.key, {
              provider: OAUTH_PROVIDER_LABELS[startError.provider],
            })}
          </p>
        )}
        {providers.data.map((provider) => {
          const label = OAUTH_PROVIDER_LABELS[provider];
          return (
            <button
              key={provider}
              type="button"
              disabled={starting !== null}
              onClick={() => void start(provider)}
              className="inline-flex w-full items-center justify-center rounded-md border border-slate-300 bg-white px-4 py-2.5 text-sm font-medium text-slate-900 hover:bg-slate-50 disabled:cursor-not-allowed disabled:opacity-60"
            >
              {starting === provider
                ? t("login.oauth.redirecting", { provider: label })
                : t("login.oauth.continueWith", { provider: label })}
            </button>
          );
        })}
      </div>
    </>
  );
}
