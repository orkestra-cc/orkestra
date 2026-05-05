import { useState, type FormEvent } from 'react';
import { Link, useNavigate, useSearchParams } from 'react-router-dom';
import { useMutation } from '@tanstack/react-query';
import { useTranslation } from 'react-i18next';

import {
  login,
  mfaLoginVerify,
  type LoginResult,
  type MfaLoginVerifyResult,
} from '@/api/auth';
import { useAuth } from '@/auth/useAuth';

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

          {loginMutation.isError && (
            <p className="rounded-md bg-red-50 px-3 py-2 text-sm text-red-700" role="alert">
              {loginMutation.error.message}
            </p>
          )}

          <button
            type="submit"
            disabled={loginMutation.isPending}
            className="inline-flex w-full items-center justify-center rounded-md bg-slate-900 px-4 py-2.5 text-sm font-medium text-white hover:bg-slate-700 disabled:cursor-not-allowed disabled:bg-slate-400"
          >
            {loginMutation.isPending ? t('login.submitting') : t('login.submit')}
          </button>

          <div className="flex items-center justify-between text-sm">
            <Link to="/forgot-password" className="text-slate-600 underline hover:text-slate-900">
              {t('login.forgot')}
            </Link>
            <Link to="/signup" className="text-slate-600 underline hover:text-slate-900">
              {t('login.signupLink')}
            </Link>
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
