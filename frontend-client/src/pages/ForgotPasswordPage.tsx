import { useState, type FormEvent } from 'react';
import { Link } from 'react-router-dom';
import { useMutation } from '@tanstack/react-query';
import { useTranslation } from 'react-i18next';

import { forgotPassword } from '@/api/auth';

// Single-screen flow: email form → neutral confirmation. Backend always
// returns 200 to defeat enumeration; the UI shows the same message
// whether or not the email exists.
export function ForgotPasswordPage() {
  const { t } = useTranslation();
  const [email, setEmail] = useState('');
  const mutation = useMutation({ mutationFn: forgotPassword });

  function onSubmit(e: FormEvent<HTMLFormElement>) {
    e.preventDefault();
    if (!email.trim()) return;
    mutation.mutate(email.trim());
  }

  if (mutation.isSuccess) {
    return (
      <section className="mx-auto max-w-md px-6 py-24 text-center">
        <h1 className="mb-3 text-3xl font-semibold tracking-tight">
          {t('forgot.successTitle')}
        </h1>
        <p className="mb-8 text-slate-600">{t('forgot.successBody')}</p>
        <Link to="/login" className="text-slate-600 underline hover:text-slate-900">
          {t('forgot.backToLogin')}
        </Link>
      </section>
    );
  }

  return (
    <section className="mx-auto max-w-md px-6 py-16">
      <h1 className="mb-2 text-3xl font-semibold tracking-tight">{t('forgot.title')}</h1>
      <p className="mb-8 text-slate-600">{t('forgot.subtitle')}</p>

      <form onSubmit={onSubmit} className="space-y-5" noValidate>
        <div>
          <label htmlFor="email" className="mb-1 block text-sm font-medium text-slate-700">
            {t('forgot.email')}
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
        <button
          type="submit"
          disabled={mutation.isPending}
          className="inline-flex w-full items-center justify-center rounded-md bg-slate-900 px-4 py-2.5 text-sm font-medium text-white hover:bg-slate-700 disabled:cursor-not-allowed disabled:bg-slate-400"
        >
          {mutation.isPending ? t('forgot.submitting') : t('forgot.submit')}
        </button>
      </form>
    </section>
  );
}
