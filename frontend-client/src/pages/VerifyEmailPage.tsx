import { useEffect, useState, type FormEvent } from 'react';
import { Link, useSearchParams } from 'react-router-dom';
import { useMutation } from '@tanstack/react-query';
import { useTranslation } from 'react-i18next';

import { resendVerificationEmail, verifyEmailToken } from '@/api/verifyEmail';

// Three-state landing page. The token is single-use and 24-hour TTL'd
// (operator_email_tokens / client_email_tokens TTL index in the auth
// module), so reloading after success would 400; the success branch
// stays sticky once entered.
type Status = 'pending' | 'success' | 'error' | 'missing';

export function VerifyEmailPage() {
  const { t } = useTranslation();
  const [params] = useSearchParams();
  const token = params.get('token');
  const [status, setStatus] = useState<Status>(() => (token ? 'pending' : 'missing'));

  useEffect(() => {
    if (!token) return;
    let cancelled = false;
    void (async () => {
      try {
        await verifyEmailToken(token);
        if (!cancelled) setStatus('success');
      } catch {
        if (!cancelled) setStatus('error');
      }
    })();
    return () => {
      cancelled = true;
    };
  }, [token]);

  return (
    <section className="mx-auto max-w-md px-6 py-24 text-center">
      <h1 className="mb-3 text-3xl font-semibold tracking-tight">{t('verify.title')}</h1>

      {status === 'pending' && (
        <p className="text-slate-600" role="status">
          {t('verify.pending')}
        </p>
      )}

      {status === 'missing' && (
        <p className="text-red-700" role="alert">
          {t('verify.missingToken')}
        </p>
      )}

      {status === 'success' && (
        <>
          <p className="mb-6 text-lg font-medium text-emerald-700">{t('verify.successTitle')}</p>
          <p className="mb-8 text-slate-600">{t('verify.successBody')}</p>
          <Link
            to="/signin"
            className="inline-flex items-center rounded-md bg-slate-900 px-4 py-2.5 text-sm font-medium text-white hover:bg-slate-700"
          >
            {t('verify.signinCta')}
          </Link>
        </>
      )}

      {status === 'error' && (
        <>
          <p className="mb-2 text-lg font-medium text-red-700">{t('verify.errorTitle')}</p>
          <p className="mb-8 text-slate-600">{t('verify.errorBody')}</p>
          <ResendForm />
        </>
      )}
    </section>
  );
}

function ResendForm() {
  const { t } = useTranslation();
  const [email, setEmail] = useState('');
  const resend = useMutation({ mutationFn: resendVerificationEmail });

  function onSubmit(e: FormEvent<HTMLFormElement>) {
    e.preventDefault();
    if (!email.trim()) return;
    resend.mutate(email.trim());
  }

  if (resend.isSuccess) {
    return (
      <p className="rounded-md bg-emerald-50 px-3 py-2 text-sm text-emerald-700" role="status">
        {t('verify.resendSent')}
      </p>
    );
  }

  return (
    <form onSubmit={onSubmit} className="mx-auto max-w-sm space-y-3 text-left">
      <h2 className="text-base font-medium text-slate-900">{t('verify.resendTitle')}</h2>
      <label htmlFor="resend-email" className="block text-sm font-medium text-slate-700">
        {t('verify.resendEmailLabel')}
      </label>
      <input
        id="resend-email"
        type="email"
        autoComplete="email"
        required
        value={email}
        onChange={(e) => setEmail(e.target.value)}
        className="block w-full rounded-md border border-slate-300 bg-white px-3 py-2 text-sm focus:border-slate-500 focus:outline-none focus:ring-1 focus:ring-slate-500"
      />
      <button
        type="submit"
        disabled={resend.isPending}
        className="inline-flex w-full items-center justify-center rounded-md bg-slate-900 px-4 py-2 text-sm font-medium text-white hover:bg-slate-700 disabled:cursor-not-allowed disabled:bg-slate-400"
      >
        {t('verify.resendSubmit')}
      </button>
    </form>
  );
}
