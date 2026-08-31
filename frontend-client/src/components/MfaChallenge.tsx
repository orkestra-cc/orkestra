import { useState, type FormEvent } from "react";
import { useMutation } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";

import { mfaLoginVerify, type MfaLoginVerifyResult } from "@/api/auth";

export interface MfaChallengeProps {
  mfaToken: string;
  onCancel: () => void;
  onSuccess: (result: MfaLoginVerifyResult) => void;
}

// The TOTP / backup-code step of a partial login. Shared by the password
// path (LoginPage) and the OAuth continuation (OAuthCallbackPage). The
// challenge id lives only in the caller's component state — never in
// router state or storage — and is one-shot on the backend.
export function MfaChallenge({
  mfaToken,
  onCancel,
  onSuccess,
}: MfaChallengeProps) {
  const { t } = useTranslation();
  const [code, setCode] = useState("");
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
        {t("login.mfa.prompt")}
      </p>
      <div>
        <label
          htmlFor="mfa-code"
          className="mb-1 block text-sm font-medium text-slate-700"
        >
          {useBackup ? t("login.mfa.backupCode") : t("login.mfa.code")}
        </label>
        <input
          id="mfa-code"
          type="text"
          inputMode={useBackup ? "text" : "numeric"}
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
        {t("login.mfa.useBackup")}
      </label>

      {verify.isError && (
        <p
          className="rounded-md bg-red-50 px-3 py-2 text-sm text-red-700"
          role="alert"
        >
          {verify.error.message}
        </p>
      )}

      <div className="flex gap-3">
        <button
          type="button"
          onClick={onCancel}
          className="flex-1 rounded-md border border-slate-300 px-4 py-2.5 text-sm font-medium text-slate-700 hover:bg-slate-50"
        >
          {t("login.mfa.cancel")}
        </button>
        <button
          type="submit"
          disabled={verify.isPending}
          className="flex-1 rounded-md bg-slate-900 px-4 py-2.5 text-sm font-medium text-white hover:bg-slate-700 disabled:cursor-not-allowed disabled:bg-slate-400"
        >
          {verify.isPending ? t("login.mfa.submitting") : t("login.mfa.submit")}
        </button>
      </div>
    </form>
  );
}
