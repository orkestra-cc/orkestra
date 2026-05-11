import { useState, type FormEvent } from 'react';
import { Link, useSearchParams } from 'react-router-dom';
import { useMutation } from '@tanstack/react-query';
import { useTranslation } from 'react-i18next';

import { resetPassword } from '@/api/auth';

// /reset-password?token=... — three sticky states (form → success →
// error/missing). Same pattern as VerifyEmailPage but writes a new
// password instead of consuming a verify token.
export function ResetPasswordPage() {
  const { t } = useTranslation();
  const [params] = useSearchParams();
  const token = params.get('token');
  const [password, setPassword] = useState('');
  const [confirm, setConfirm] = useState('');
  const [validationError, setValidationError] = useState<string | null>(null);

  const mutation = useMutation({
    mutationFn: () => resetPassword(token ?? '', password),
  });

  if (!token) {
    return (
      <section className="mx-auto max-w-md px-6 py-24 text-center">
        <h1 className="mb-3 text-3xl font-semibold tracking-tight">{t('reset.title')}</h1>
        <p className="text-red-700" role="alert">
          {t('reset.missingToken')}
        </p>
      </section>
    );
  }

  if (mutation.isSuccess) {
    return (
      <section className="mx-auto max-w-md px-6 py-24 text-center">
        <h1 className="mb-3 text-3xl font-semibold tracking-tight">{t('reset.successTitle')}</h1>
        <p className="mb-8 text-slate-600">{t('reset.successBody')}</p>
        <Link
          to="/login"
          className="inline-flex items-center rounded-md bg-slate-900 px-4 py-2.5 text-sm font-medium text-white hover:bg-slate-700"
        >
          {t('reset.signinCta')}
        </Link>
      </section>
    );
  }

  function onSubmit(e: FormEvent<HTMLFormElement>) {
    e.preventDefault();
    setValidationError(null);
    if (password.length < 10) {
      setValidationError(t('reset.errorTooShort'));
      return;
    }
    if (password !== confirm) {
      setValidationError(t('reset.errorMismatch'));
      return;
    }
    mutation.mutate();
  }

  return (
    <section className="mx-auto max-w-md px-6 py-16">
      <h1 className="mb-2 text-3xl font-semibold tracking-tight">{t('reset.title')}</h1>
      <p className="mb-8 text-slate-600">{t('reset.subtitle')}</p>

      <form onSubmit={onSubmit} className="space-y-5" noValidate>
        <div>
          <label htmlFor="new-password" className="mb-1 block text-sm font-medium text-slate-700">
            {t('reset.newPassword')}
          </label>
          <input
            id="new-password"
            type="password"
            autoComplete="new-password"
            required
            value={password}
            onChange={(e) => setPassword(e.target.value)}
            className="block w-full rounded-md border border-slate-300 bg-white px-3 py-2 text-sm focus:border-slate-500 focus:outline-none focus:ring-1 focus:ring-slate-500"
          />
          <p className="mt-1 text-xs text-slate-500">{t('reset.passwordHint')}</p>
        </div>
        <div>
          <label htmlFor="confirm-password" className="mb-1 block text-sm font-medium text-slate-700">
            {t('reset.confirmPassword')}
          </label>
          <input
            id="confirm-password"
            type="password"
            autoComplete="new-password"
            required
            value={confirm}
            onChange={(e) => setConfirm(e.target.value)}
            className="block w-full rounded-md border border-slate-300 bg-white px-3 py-2 text-sm focus:border-slate-500 focus:outline-none focus:ring-1 focus:ring-slate-500"
          />
        </div>

        {(validationError || mutation.isError) && (
          <p className="rounded-md bg-red-50 px-3 py-2 text-sm text-red-700" role="alert">
            {validationError ?? mutation.error?.message ?? t('error.generic')}
          </p>
        )}

        <button
          type="submit"
          disabled={mutation.isPending}
          className="inline-flex w-full items-center justify-center rounded-md bg-slate-900 px-4 py-2.5 text-sm font-medium text-white hover:bg-slate-700 disabled:cursor-not-allowed disabled:bg-slate-400"
        >
          {mutation.isPending ? t('reset.submitting') : t('reset.submit')}
        </button>
      </form>
    </section>
  );
}
