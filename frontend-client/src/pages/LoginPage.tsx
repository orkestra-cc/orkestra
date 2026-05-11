import { useEffect, useState, type FormEvent } from 'react';
import { Link, useNavigate, useSearchParams } from 'react-router-dom';
import { useMutation, useQuery } from '@tanstack/react-query';
import { useTranslation } from 'react-i18next';

import {
  fetchAuthPolicy,
  login,
  mfaLoginVerify,
  type LoginResult,
  type MfaLoginVerifyResult,
} from '@/api/auth';
import { resendVerificationEmail } from '@/api/verifyEmail';
import { useAuth } from '@/auth/useAuth';

// Backend marks the "address not verified" 403 with code="email_not_verified"
// (see auth/handlers/password_handler.go::mapPasswordError). We discriminate
// on the code, not on the localized detail string.
type ApiErrorWithCode = Error & { code?: string; status?: number };

function isEmailNotVerified(err: unknown): boolean {
  const e = err as ApiErrorWithCode | null;
  return !!e && e.code === 'email_not_verified';
}

// Two-state page: credentials (default) → mfa-required (after a partial
// login response carries requiresMfa=true). State lives in the local
// component because a navigation away should drop the in-flight
// challenge — the backend's mfaToken is short-lived and one-shot
// anyway. On full success (either branch) we stamp the in-memory token
// + session marker via AuthProvider.signIn and redirect to ?next= or
// /account.
type Stage =
  | { name: 'credentials' }
  | { name: 'mfa'; mfaToken: string; webauthnAvailable: boolean };

export function LoginPage() {
  const { t } = useTranslation();
  const { signIn } = useAuth();
  const navigate = useNavigate();
  const [params] = useSearchParams();
  const next = params.get('next') ?? '/account';

  const [stage, setStage] = useState<Stage>({ name: 'credentials' });
  const [email, setEmail] = useState('');
  const [password, setPassword] = useState('');

  // Public policy: drives the maintenance banner and hides the signup
  // link when self-service registration is also off. Falls open on any
  // network failure inside fetchAuthPolicy itself.
  const { data: policy } = useQuery({
    queryKey: ['authPolicy'],
    queryFn: fetchAuthPolicy,
    staleTime: 30_000,
  });
  const loginEnabled = policy?.loginEnabled ?? true;
  const registrationEnabled = policy?.registrationEnabled ?? true;

  function complete(token: string) {
    signIn(token);
    navigate(decodeURIComponent(next), { replace: true });
  }

  const loginMutation = useMutation<LoginResult, Error, void>({
    mutationFn: () => login({ email: email.trim(), password }),
    onSuccess: (result) => {
      if (result.kind === 'mfa_required') {
        setStage({
          name: 'mfa',
          mfaToken: result.mfaToken,
          webauthnAvailable: result.webauthnAvailable,
        });
        return;
      }
      complete(result.accessToken);
    },
  });

  function onSubmitCredentials(e: FormEvent<HTMLFormElement>) {
    e.preventDefault();
    if (!email.trim() || !password) return;
    loginMutation.mutate();
  }

  return (
    <section className="mx-auto max-w-md px-6 py-16">
      <h1 className="mb-2 text-3xl font-semibold tracking-tight">{t('login.title')}</h1>
      <p className="mb-8 text-slate-600">{t('login.subtitle')}</p>

      {!loginEnabled && stage.name === 'credentials' && (
        <div
          className="mb-6 rounded-md border border-amber-200 bg-amber-50 px-4 py-3 text-sm text-amber-900"
          role="alert"
        >
          {t('login.disabled')}
        </div>
      )}

      {stage.name === 'credentials' ? (
        <form onSubmit={onSubmitCredentials} noValidate className="space-y-5">
          <div>
            <label htmlFor="email" className="mb-1 block text-sm font-medium text-slate-700">
              {t('login.email')}
            </label>
            <input
              id="email"
              type="email"
              autoComplete="email"
              required
              value={email}
              onChange={(e) => setEmail(e.target.value)}
              className="block w-full rounded-md border border-slate-300 bg-white px-3 py-2 text-sm focus:border-slate-500 focus:outline-none focus:ring-1 focus:ring-slate-500"
            />
          </div>
          <div>
            <label htmlFor="password" className="mb-1 block text-sm font-medium text-slate-700">
              {t('login.password')}
            </label>
            <input
              id="password"
              type="password"
              autoComplete="current-password"
              required
              value={password}
              onChange={(e) => setPassword(e.target.value)}
              className="block w-full rounded-md border border-slate-300 bg-white px-3 py-2 text-sm focus:border-slate-500 focus:outline-none focus:ring-1 focus:ring-slate-500"
            />
          </div>

          {loginMutation.isError && !isEmailNotVerified(loginMutation.error) && (
            <p className="rounded-md bg-red-50 px-3 py-2 text-sm text-red-700" role="alert">
              {loginMutation.error.message}
            </p>
          )}

          {loginMutation.isError && isEmailNotVerified(loginMutation.error) && (
            <EmailNotVerifiedNotice email={email.trim()} />
          )}

          <button
            type="submit"
            disabled={loginMutation.isPending || !loginEnabled}
            className="inline-flex w-full items-center justify-center rounded-md bg-slate-900 px-4 py-2.5 text-sm font-medium text-white hover:bg-slate-700 disabled:cursor-not-allowed disabled:bg-slate-400"
          >
            {loginMutation.isPending ? t('login.submitting') : t('login.submit')}
          </button>

          <div className="flex items-center justify-between text-sm">
            <Link to="/forgot-password" className="text-slate-600 underline hover:text-slate-900">
              {t('login.forgot')}
            </Link>
            {registrationEnabled && (
              <Link to="/signup" className="text-slate-600 underline hover:text-slate-900">
                {t('login.signupLink')}
              </Link>
            )}
          </div>
        </form>
      ) : (
        <MfaChallenge
          mfaToken={stage.mfaToken}
          onCancel={() => setStage({ name: 'credentials' })}
          onSuccess={(result) => complete(result.accessToken)}
        />
      )}
    </section>
  );
}

// Inline panel rendered when login returns code="email_not_verified".
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
      <p className="font-medium">{t('login.notVerified.title')}</p>
      <p className="mt-1 text-amber-800">{t('login.notVerified.body')}</p>

      {resend.isSuccess ? (
        <p className="mt-3 rounded-md bg-emerald-50 px-3 py-2 text-emerald-700" role="status">
          {t('login.notVerified.resendDone')}
        </p>
      ) : (
        <button
          type="button"
          disabled={!canSend}
          onClick={() => email && resend.mutate(email)}
          className="mt-3 inline-flex items-center justify-center rounded-md bg-slate-900 px-3 py-2 text-sm font-medium text-white hover:bg-slate-700 disabled:cursor-not-allowed disabled:bg-slate-400"
        >
          {cooldownLeft > 0
            ? t('login.notVerified.resendCooldown', { seconds: cooldownLeft })
            : resend.isPending
              ? t('login.notVerified.resendSending')
              : t('login.notVerified.resendCta')}
        </button>
      )}
    </div>
  );
}

interface MfaChallengeProps {
  mfaToken: string;
  onCancel: () => void;
  onSuccess: (result: MfaLoginVerifyResult) => void;
}

function MfaChallenge({ mfaToken, onCancel, onSuccess }: MfaChallengeProps) {
  const { t } = useTranslation();
  const [code, setCode] = useState('');
  const [useBackup, setUseBackup] = useState(false);

  const verify = useMutation<MfaLoginVerifyResult, Error, void>({
    mutationFn: () =>
      mfaLoginVerify({
        challengeId: mfaToken,
        code: code.trim(),
        useBackup,
      }),
    onSuccess,
  });

  function onSubmit(e: FormEvent<HTMLFormElement>) {
    e.preventDefault();
    if (!code.trim()) return;
    verify.mutate();
  }

  return (
    <form onSubmit={onSubmit} noValidate className="space-y-5">
      <p className="rounded-md bg-amber-50 px-3 py-2 text-sm text-amber-800">
        {t('login.mfa.prompt')}
      </p>
      <div>
        <label htmlFor="mfa-code" className="mb-1 block text-sm font-medium text-slate-700">
          {useBackup ? t('login.mfa.backupCode') : t('login.mfa.code')}
        </label>
        <input
          id="mfa-code"
          type="text"
          inputMode={useBackup ? 'text' : 'numeric'}
          autoComplete="one-time-code"
          autoFocus
          required
          value={code}
          onChange={(e) => setCode(e.target.value)}
          className="block w-full rounded-md border border-slate-300 bg-white px-3 py-2 text-base tracking-widest focus:border-slate-500 focus:outline-none focus:ring-1 focus:ring-slate-500"
        />
      </div>

      <label className="flex items-center gap-2 text-sm text-slate-700">
        <input
          type="checkbox"
          checked={useBackup}
          onChange={(e) => setUseBackup(e.target.checked)}
          className="h-4 w-4 rounded border-slate-300 text-slate-900 focus:ring-slate-500"
        />
        {t('login.mfa.useBackup')}
      </label>

      {verify.isError && (
        <p className="rounded-md bg-red-50 px-3 py-2 text-sm text-red-700" role="alert">
          {verify.error.message}
        </p>
      )}

      <div className="flex gap-3">
        <button
          type="button"
          onClick={onCancel}
          className="flex-1 rounded-md border border-slate-300 px-4 py-2.5 text-sm font-medium text-slate-700 hover:bg-slate-50"
        >
          {t('login.mfa.cancel')}
        </button>
        <button
          type="submit"
          disabled={verify.isPending}
          className="flex-1 rounded-md bg-slate-900 px-4 py-2.5 text-sm font-medium text-white hover:bg-slate-700 disabled:cursor-not-allowed disabled:bg-slate-400"
        >
          {verify.isPending ? t('login.mfa.submitting') : t('login.mfa.submit')}
        </button>
      </div>
    </form>
  );
}
