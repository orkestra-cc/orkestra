import { useState, type FormEvent } from 'react';
import { Link, useNavigate } from 'react-router-dom';
import { useMutation, useQuery } from '@tanstack/react-query';
import { useTranslation } from 'react-i18next';

import { changePassword, getMfaStatus, type MfaStatus } from '@/api/auth';
import { useAuth } from '@/auth/useAuth';

// /account/security — change password (most common task) + MFA status
// summary with deep-link to enrolment. Password change revokes the
// current session server-side, so on success we must signOut and route
// the user back to /login.
export function AccountSecurityPage() {
  const { t } = useTranslation();
  return (
    <section className="mx-auto max-w-3xl space-y-12 px-6 py-16">
      <header>
        <Link to="/account" className="mb-4 inline-block text-sm text-slate-600 hover:underline">
          ← {t('account.back')}
        </Link>
        <h1 className="text-3xl font-semibold tracking-tight">{t('account.security.title')}</h1>
      </header>

      <ChangePasswordCard />
      <MfaCard />
    </section>
  );
}

function ChangePasswordCard() {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const { signOut } = useAuth();
  const [current, setCurrent] = useState('');
  const [next, setNext] = useState('');
  const [confirm, setConfirm] = useState('');
  const [validationError, setValidationError] = useState<string | null>(null);

  const mutation = useMutation({
    mutationFn: () => changePassword(current, next),
    onSuccess: async () => {
      // Backend revoked the current session — drop the in-memory token
      // and route to login so the user can sign in with the new pwd.
      await signOut();
      navigate('/login?next=%2Faccount%2Fsecurity', { replace: true });
    },
  });

  function onSubmit(e: FormEvent<HTMLFormElement>) {
    e.preventDefault();
    setValidationError(null);
    if (next.length < 10) {
      setValidationError(t('reset.errorTooShort'));
      return;
    }
    if (next !== confirm) {
      setValidationError(t('reset.errorMismatch'));
      return;
    }
    mutation.mutate();
  }

  return (
    <article className="rounded-lg border border-slate-200 bg-white p-6 shadow-sm">
      <h2 className="mb-1 text-lg font-semibold text-slate-900">
        {t('account.changePassword.title')}
      </h2>
      <p className="mb-6 text-sm text-slate-600">{t('account.changePassword.subtitle')}</p>

      <form onSubmit={onSubmit} className="space-y-4" noValidate>
        <PasswordField
          id="current-password"
          label={t('account.changePassword.current')}
          autoComplete="current-password"
          value={current}
          onChange={setCurrent}
        />
        <PasswordField
          id="new-password"
          label={t('account.changePassword.new')}
          hint={t('reset.passwordHint')}
          autoComplete="new-password"
          value={next}
          onChange={setNext}
        />
        <PasswordField
          id="confirm-password"
          label={t('account.changePassword.confirm')}
          autoComplete="new-password"
          value={confirm}
          onChange={setConfirm}
        />

        {(validationError || mutation.isError) && (
          <p className="rounded-md bg-red-50 px-3 py-2 text-sm text-red-700" role="alert">
            {validationError ?? mutation.error?.message ?? t('error.generic')}
          </p>
        )}

        <button
          type="submit"
          disabled={mutation.isPending}
          className="inline-flex items-center justify-center rounded-md bg-slate-900 px-4 py-2 text-sm font-medium text-white hover:bg-slate-700 disabled:cursor-not-allowed disabled:bg-slate-400"
        >
          {mutation.isPending ? t('account.changePassword.submitting') : t('account.changePassword.submit')}
        </button>
      </form>
    </article>
  );
}

function MfaCard() {
  const { t } = useTranslation();
  const { data, isLoading, isError } = useQuery<MfaStatus>({
    queryKey: ['mfa-status'],
    queryFn: ({ signal }) => getMfaStatus(signal),
    staleTime: 30_000,
    retry: false,
  });

  return (
    <article className="rounded-lg border border-slate-200 bg-white p-6 shadow-sm">
      <h2 className="mb-1 text-lg font-semibold text-slate-900">{t('account.mfa.title')}</h2>
      <p className="mb-6 text-sm text-slate-600">{t('account.mfa.subtitle')}</p>

      {isLoading && <p className="text-sm text-slate-500">{t('loading')}</p>}
      {isError && (
        <p className="rounded-md bg-red-50 px-3 py-2 text-sm text-red-700" role="alert">
          {t('error.generic')}
        </p>
      )}

      {data && (
        <div className="space-y-4">
          <dl className="grid grid-cols-1 gap-4 sm:grid-cols-2">
            <Field
              label={t('account.mfa.status')}
              value={t(`account.mfa.statusValue.${data.status}`, data.status)}
            />
            {data.type && (
              <Field label={t('account.mfa.type')} value={data.type.toUpperCase()} />
            )}
            <Field
              label={t('account.mfa.backupCodesRemaining')}
              value={String(data.backupCodesRemaining)}
            />
            {data.requiresMfa && (
              <Field
                label={t('account.mfa.required')}
                value={t('account.mfa.requiredYes')}
              />
            )}
          </dl>

          {data.requiresMfa && data.graceExpiresAt && (
            <p className="rounded-md bg-amber-50 px-3 py-2 text-sm text-amber-800">
              {t('account.mfa.graceWarning', {
                date: new Date(data.graceExpiresAt).toLocaleDateString(),
              })}
            </p>
          )}

          {(data.status === 'not_required' || data.status === 'required') && (
            <Link
              to="/account/security/mfa"
              className="inline-flex items-center rounded-md bg-slate-900 px-4 py-2 text-sm font-medium text-white hover:bg-slate-700"
            >
              {t('account.mfa.enrol')}
            </Link>
          )}
        </div>
      )}
    </article>
  );
}

interface PasswordFieldProps {
  id: string;
  label: string;
  value: string;
  onChange: (v: string) => void;
  autoComplete: string;
  hint?: string;
}

function PasswordField({ id, label, value, onChange, autoComplete, hint }: PasswordFieldProps) {
  return (
    <div>
      <label htmlFor={id} className="mb-1 block text-sm font-medium text-slate-700">
        {label}
      </label>
      <input
        id={id}
        type="password"
        autoComplete={autoComplete}
        required
        value={value}
        onChange={(e) => onChange(e.target.value)}
        className="block w-full rounded-md border border-slate-300 bg-white px-3 py-2 text-sm focus:border-slate-500 focus:outline-none focus:ring-1 focus:ring-slate-500"
      />
      {hint && <p className="mt-1 text-xs text-slate-500">{hint}</p>}
    </div>
  );
}

function Field({ label, value }: { label: string; value: string }) {
  return (
    <div>
      <dt className="mb-1 text-xs font-medium uppercase tracking-wider text-slate-500">{label}</dt>
      <dd className="text-sm text-slate-900">{value}</dd>
    </div>
  );
}
