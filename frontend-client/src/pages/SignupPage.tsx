import { useState, type FormEvent } from 'react';
import { Link } from 'react-router-dom';
import { useMutation, useQuery } from '@tanstack/react-query';
import { useTranslation } from 'react-i18next';

import { fetchAuthPolicy, register, type RegisterInput, type RegisterResult } from '@/api/auth';
import { resendVerificationEmail } from '@/api/verifyEmail';

interface ApiError extends Error {
  status: number;
  code?: string;
}

interface FieldErrors {
  email?: string;
  password?: string;
  fullName?: string;
  terms?: string;
}

export function SignupPage() {
  const { t } = useTranslation();

  const [form, setForm] = useState({
    email: '',
    password: '',
    fullName: '',
    terms: false,
  });
  const [errors, setErrors] = useState<FieldErrors>({});
  const [submittedEmail, setSubmittedEmail] = useState<string | null>(null);

  // Public policy drives the kill-switch banner + min-length floor.
  // Falls open on any error inside fetchAuthPolicy itself.
  const { data: policy } = useQuery({
    queryKey: ['authPolicy'],
    queryFn: fetchAuthPolicy,
    staleTime: 30_000,
  });
  const registrationEnabled = policy?.registrationEnabled ?? true;
  const passwordMinLength = policy?.passwordMinLength ?? 10;

  const mutation = useMutation<RegisterResult, ApiError, RegisterInput>({
    mutationFn: register,
    onSuccess: (_data, variables) => {
      setSubmittedEmail(variables.email);
    },
  });

  function validate(): FieldErrors {
    const next: FieldErrors = {};
    if (!form.email.trim()) {
      next.email = t('signup.errorEmailRequired');
    } else if (!/^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(form.email.trim())) {
      next.email = t('signup.errorEmailInvalid');
    }
    if (!form.password) {
      next.password = t('signup.errorPasswordRequired');
    } else if (form.password.length < passwordMinLength) {
      next.password = t('signup.errorPasswordTooShort');
    }
    if (!form.fullName.trim()) next.fullName = t('signup.errorFullNameRequired');
    if (!form.terms) next.terms = t('signup.errorTermsRequired');
    return next;
  }

  function onSubmit(e: FormEvent<HTMLFormElement>) {
    e.preventDefault();
    const validation = validate();
    setErrors(validation);
    if (Object.keys(validation).length > 0) return;
    mutation.mutate({
      email: form.email.trim(),
      password: form.password,
      fullName: form.fullName.trim(),
    });
  }

  if (submittedEmail) {
    return <SignupSuccess email={submittedEmail} />;
  }

  return (
    <section className="mx-auto max-w-md px-6 py-16">
      <h1 className="mb-2 text-3xl font-semibold tracking-tight">{t('signup.title')}</h1>
      <p className="mb-8 text-slate-600">{t('signup.subtitle')}</p>

      {!registrationEnabled && (
        <div
          className="mb-6 rounded-md border border-amber-200 bg-amber-50 px-4 py-3 text-sm text-amber-900"
          role="alert"
        >
          {t('signup.disabled')}
        </div>
      )}

      <form onSubmit={onSubmit} noValidate className="space-y-5">
        <Field
          id="email"
          label={t('signup.email')}
          type="email"
          autoComplete="email"
          value={form.email}
          onChange={(v) => setForm({ ...form, email: v })}
          error={errors.email}
        />
        <Field
          id="password"
          label={t('signup.password')}
          hint={t('signup.passwordHint')}
          type="password"
          autoComplete="new-password"
          value={form.password}
          onChange={(v) => setForm({ ...form, password: v })}
          error={errors.password}
        />
        <Field
          id="fullName"
          label={t('signup.fullName')}
          autoComplete="name"
          value={form.fullName}
          onChange={(v) => setForm({ ...form, fullName: v })}
          error={errors.fullName}
        />

        <label className="flex items-start gap-2 text-sm text-slate-700">
          <input
            type="checkbox"
            checked={form.terms}
            onChange={(e) => setForm({ ...form, terms: e.target.checked })}
            className="mt-0.5 h-4 w-4 rounded border-slate-300 text-slate-900 focus:ring-slate-500"
          />
          <span>{t('signup.terms')}</span>
        </label>
        {errors.terms && (
          <p className="text-sm text-red-600" role="alert">
            {errors.terms}
          </p>
        )}

        {mutation.isError && (
          <p className="rounded-md bg-red-50 px-3 py-2 text-sm text-red-700" role="alert">
            {mutation.error.message}
          </p>
        )}

        <button
          type="submit"
          disabled={mutation.isPending || !registrationEnabled}
          className="inline-flex w-full items-center justify-center rounded-md bg-slate-900 px-4 py-2.5 text-sm font-medium text-white hover:bg-slate-700 disabled:cursor-not-allowed disabled:bg-slate-400"
        >
          {mutation.isPending ? t('signup.submitting') : t('signup.submit')}
        </button>

        <p className="text-center text-sm text-slate-600">
          {t('signup.haveAccount')}{' '}
          <Link to="/login" className="font-medium text-slate-900 underline">
            {t('signup.signinLink')}
          </Link>
        </p>
      </form>
    </section>
  );
}

function SignupSuccess({ email }: { email: string }) {
  const { t } = useTranslation();
  const resend = useMutation({ mutationFn: resendVerificationEmail });
  return (
    <section className="mx-auto max-w-md px-6 py-24 text-center">
      <h1 className="mb-3 text-3xl font-semibold tracking-tight">{t('signup.successTitle')}</h1>
      <p className="mb-8 text-slate-600">{t('signup.successBody', { email })}</p>
      <button
        type="button"
        onClick={() => resend.mutate(email)}
        disabled={resend.isPending || resend.isSuccess}
        className="text-sm text-slate-600 underline hover:text-slate-900 disabled:cursor-not-allowed disabled:no-underline"
      >
        {resend.isSuccess ? t('verify.resendSent') : t('signup.successResend')}
      </button>
    </section>
  );
}

interface FieldProps {
  id: string;
  label: string;
  type?: string;
  autoComplete?: string;
  value: string;
  onChange: (v: string) => void;
  error?: string;
  hint?: string;
}

function Field({ id, label, type = 'text', autoComplete, value, onChange, error, hint }: FieldProps) {
  return (
    <div>
      <label htmlFor={id} className="mb-1 block text-sm font-medium text-slate-700">
        {label}
      </label>
      <input
        id={id}
        name={id}
        type={type}
        autoComplete={autoComplete}
        value={value}
        onChange={(e) => onChange(e.target.value)}
        aria-invalid={error ? 'true' : 'false'}
        aria-describedby={hint ? `${id}-hint` : error ? `${id}-error` : undefined}
        className="block w-full rounded-md border border-slate-300 bg-white px-3 py-2 text-sm focus:border-slate-500 focus:outline-none focus:ring-1 focus:ring-slate-500"
      />
      {hint && !error && (
        <p id={`${id}-hint`} className="mt-1 text-xs text-slate-500">
          {hint}
        </p>
      )}
      {error && (
        <p id={`${id}-error`} className="mt-1 text-xs text-red-600" role="alert">
          {error}
        </p>
      )}
    </div>
  );
}
