import { useState, type FormEvent } from 'react';
import { Link, useNavigate } from 'react-router-dom';
import { useMutation } from '@tanstack/react-query';
import { useTranslation } from 'react-i18next';

import {
  mfaEnrollBegin,
  mfaEnrollConfirm,
  type MfaEnrollBegin,
  type MfaEnrollConfirm,
} from '@/api/auth';

// Three-step enrolment: begin (POST → secret + otpauth URI) → user
// scans/types into authenticator → confirm (POST {challengeId, code}
// → backup codes). Backup codes display once and are only re-issuable
// by removing+re-enrolling the factor, so we make the user copy them
// before navigating away.
type Stage =
  | { kind: 'idle' }
  | { kind: 'pending'; data: MfaEnrollBegin }
  | { kind: 'done'; backupCodes: string[] };

export function MfaEnrolPage() {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const [stage, setStage] = useState<Stage>({ kind: 'idle' });

  const begin = useMutation<MfaEnrollBegin, Error, void>({
    mutationFn: () => mfaEnrollBegin(),
    onSuccess: (data) => setStage({ kind: 'pending', data }),
  });

  return (
    <section className="mx-auto max-w-2xl space-y-8 px-6 py-16">
      <header>
        <Link
          to="/account/security"
          className="mb-4 inline-block text-sm text-slate-600 hover:underline"
        >
          ← {t('account.security.title')}
        </Link>
        <h1 className="text-3xl font-semibold tracking-tight">{t('mfa.enrol.title')}</h1>
        <p className="mt-2 text-slate-600">{t('mfa.enrol.subtitle')}</p>
      </header>

      {stage.kind === 'idle' && (
        <div className="rounded-lg border border-slate-200 bg-white p-6 shadow-sm">
          <p className="mb-6 text-sm text-slate-700">{t('mfa.enrol.step1')}</p>
          {begin.isError && (
            <p className="mb-4 rounded-md bg-red-50 px-3 py-2 text-sm text-red-700" role="alert">
              {begin.error.message}
            </p>
          )}
          <button
            type="button"
            onClick={() => begin.mutate()}
            disabled={begin.isPending}
            className="inline-flex items-center rounded-md bg-slate-900 px-4 py-2 text-sm font-medium text-white hover:bg-slate-700 disabled:cursor-not-allowed disabled:bg-slate-400"
          >
            {begin.isPending ? t('mfa.enrol.starting') : t('mfa.enrol.start')}
          </button>
        </div>
      )}

      {stage.kind === 'pending' && (
        <ConfirmStage
          data={stage.data}
          onSuccess={(res) => setStage({ kind: 'done', backupCodes: res.backupCodes })}
        />
      )}

      {stage.kind === 'done' && (
        <BackupCodesPanel
          codes={stage.backupCodes}
          onContinue={() => navigate('/account/security', { replace: true })}
        />
      )}
    </section>
  );
}

interface ConfirmStageProps {
  data: MfaEnrollBegin;
  onSuccess: (res: MfaEnrollConfirm) => void;
}

function ConfirmStage({ data, onSuccess }: ConfirmStageProps) {
  const { t } = useTranslation();
  const [code, setCode] = useState('');
  const [copied, setCopied] = useState(false);

  const confirm = useMutation<MfaEnrollConfirm, Error, void>({
    mutationFn: () => mfaEnrollConfirm(data.challengeId, code.trim()),
    onSuccess,
  });

  async function copySecret() {
    try {
      await navigator.clipboard.writeText(data.secret);
      setCopied(true);
      setTimeout(() => setCopied(false), 2000);
    } catch {
      // Older browsers / non-secure context — silently no-op; the
      // secret is plain text in the input above so the user can
      // hand-copy.
    }
  }

  function onSubmit(e: FormEvent<HTMLFormElement>) {
    e.preventDefault();
    if (!code.trim()) return;
    confirm.mutate();
  }

  return (
    <div className="space-y-6">
      <div className="rounded-lg border border-slate-200 bg-white p-6 shadow-sm">
        <h2 className="mb-3 text-lg font-semibold text-slate-900">{t('mfa.enrol.step2')}</h2>
        <p className="mb-4 text-sm text-slate-600">{t('mfa.enrol.scanInstructions')}</p>

        <div className="mb-4">
          <p className="mb-1 text-xs font-medium uppercase tracking-wider text-slate-500">
            {t('mfa.enrol.secret')}
          </p>
          <div className="flex items-center gap-2">
            <code className="flex-1 break-all rounded-md bg-slate-100 px-3 py-2 font-mono text-sm">
              {data.secret}
            </code>
            <button
              type="button"
              onClick={copySecret}
              className="rounded-md border border-slate-300 px-3 py-2 text-xs font-medium text-slate-700 hover:bg-slate-50"
            >
              {copied ? t('mfa.enrol.copied') : t('mfa.enrol.copy')}
            </button>
          </div>
        </div>

        <details className="text-sm text-slate-600">
          <summary className="cursor-pointer underline">{t('mfa.enrol.uriToggle')}</summary>
          <code className="mt-2 block break-all rounded-md bg-slate-100 px-3 py-2 font-mono text-xs">
            {data.provisioningUri}
          </code>
        </details>
      </div>

      <form
        onSubmit={onSubmit}
        className="space-y-4 rounded-lg border border-slate-200 bg-white p-6 shadow-sm"
        noValidate
      >
        <h2 className="text-lg font-semibold text-slate-900">{t('mfa.enrol.step3')}</h2>
        <div>
          <label htmlFor="code" className="mb-1 block text-sm font-medium text-slate-700">
            {t('mfa.enrol.codeLabel')}
          </label>
          <input
            id="code"
            type="text"
            inputMode="numeric"
            autoComplete="one-time-code"
            autoFocus
            required
            value={code}
            onChange={(e) => setCode(e.target.value)}
            className="block w-full rounded-md border border-slate-300 bg-white px-3 py-2 text-base tracking-widest focus:border-slate-500 focus:outline-none focus:ring-1 focus:ring-slate-500"
          />
        </div>
        {confirm.isError && (
          <p className="rounded-md bg-red-50 px-3 py-2 text-sm text-red-700" role="alert">
            {confirm.error.message}
          </p>
        )}
        <button
          type="submit"
          disabled={confirm.isPending}
          className="inline-flex items-center rounded-md bg-slate-900 px-4 py-2 text-sm font-medium text-white hover:bg-slate-700 disabled:cursor-not-allowed disabled:bg-slate-400"
        >
          {confirm.isPending ? t('mfa.enrol.confirming') : t('mfa.enrol.confirm')}
        </button>
      </form>
    </div>
  );
}

interface BackupCodesProps {
  codes: string[];
  onContinue: () => void;
}

function BackupCodesPanel({ codes, onContinue }: BackupCodesProps) {
  const { t } = useTranslation();
  const [acknowledged, setAcknowledged] = useState(false);

  async function copyAll() {
    try {
      await navigator.clipboard.writeText(codes.join('\n'));
    } catch {
      // best-effort; codes are visible above for hand-copy.
    }
  }

  function downloadTxt() {
    const blob = new Blob([codes.join('\n') + '\n'], { type: 'text/plain' });
    const url = URL.createObjectURL(blob);
    const a = document.createElement('a');
    a.href = url;
    a.download = 'orkestra-backup-codes.txt';
    a.click();
    URL.revokeObjectURL(url);
  }

  return (
    <article className="rounded-lg border border-emerald-200 bg-emerald-50 p-6">
      <h2 className="mb-2 text-lg font-semibold text-emerald-900">
        {t('mfa.enrol.backupTitle')}
      </h2>
      <p className="mb-6 text-sm text-emerald-800">{t('mfa.enrol.backupSubtitle')}</p>

      <ul className="mb-6 grid grid-cols-2 gap-2 rounded-md bg-white p-4 font-mono text-sm">
        {codes.map((code) => (
          <li key={code} className="text-slate-900">
            {code}
          </li>
        ))}
      </ul>

      <div className="mb-6 flex flex-wrap gap-3">
        <button
          type="button"
          onClick={copyAll}
          className="rounded-md border border-slate-300 bg-white px-3 py-2 text-sm font-medium text-slate-700 hover:bg-slate-50"
        >
          {t('mfa.enrol.backupCopy')}
        </button>
        <button
          type="button"
          onClick={downloadTxt}
          className="rounded-md border border-slate-300 bg-white px-3 py-2 text-sm font-medium text-slate-700 hover:bg-slate-50"
        >
          {t('mfa.enrol.backupDownload')}
        </button>
      </div>

      <label className="mb-4 flex items-start gap-2 text-sm text-emerald-900">
        <input
          type="checkbox"
          checked={acknowledged}
          onChange={(e) => setAcknowledged(e.target.checked)}
          className="mt-0.5 h-4 w-4 rounded border-emerald-300 text-emerald-700 focus:ring-emerald-500"
        />
        {t('mfa.enrol.backupAcknowledge')}
      </label>

      <button
        type="button"
        onClick={onContinue}
        disabled={!acknowledged}
        className="inline-flex items-center rounded-md bg-emerald-700 px-4 py-2 text-sm font-medium text-white hover:bg-emerald-800 disabled:cursor-not-allowed disabled:bg-slate-400"
      >
        {t('mfa.enrol.backupContinue')}
      </button>
    </article>
  );
}
