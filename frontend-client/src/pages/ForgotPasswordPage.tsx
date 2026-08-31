import { useState, type FormEvent } from "react";
import { Link } from "react-router";
import { useMutation, useQuery } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";

import {
  apiErrorCode,
  fetchAuthPolicy,
  forgotPassword,
  passwordLoginUsable,
} from "@/api/auth";

// Single-screen flow: email form → neutral confirmation. The backend
// answers 200 for every account outcome (enumeration-resistant), so the
// UI shows the same message whether or not the email exists. The only
// errors are the per-surface policy answers evaluated before the lookup
// (spec §4.3): 403 auth.password_login_disabled, 503
// auth.policy_unavailable — mapped by code below, never rendered raw.
export function ForgotPasswordPage() {
  const { t } = useTranslation();
  const [email, setEmail] = useState("");
  const mutation = useMutation({ mutationFn: forgotPassword });

  // Same cached policy the login page reads. When the method is off the
  // form is hidden rather than refused on submit (G5; deviation D8).
  const { data: policy } = useQuery({
    queryKey: ["authPolicy"],
    queryFn: fetchAuthPolicy,
    staleTime: 30_000,
  });
  const passwordOn = passwordLoginUsable(policy);

  // The display may have fallen open (policy 503) while the backend still
  // gates the route: the form then shows the backend's answer (spec §4.10).
  function errorKey(e: unknown): string {
    switch (apiErrorCode(e)) {
      case "auth.password_login_disabled":
        return "forgot.passwordDisabled";
      case "auth.policy_unavailable":
        return "error.policyUnavailable";
      default:
        return "error.generic";
    }
  }

  function onSubmit(e: FormEvent<HTMLFormElement>) {
    e.preventDefault();
    if (!email.trim()) return;
    mutation.mutate(email.trim());
  }

  if (mutation.isSuccess) {
    return (
      <section className="mx-auto max-w-md px-6 py-24 text-center">
        <h1 className="mb-3 text-3xl font-semibold tracking-tight">
          {t("forgot.successTitle")}
        </h1>
        <p className="mb-8 text-slate-600">{t("forgot.successBody")}</p>
        <Link
          to="/login"
          className="text-slate-600 underline hover:text-slate-900"
        >
          {t("forgot.backToLogin")}
        </Link>
      </section>
    );
  }

  if (!passwordOn) {
    return (
      <section className="mx-auto max-w-md px-6 py-16">
        <h1 className="mb-2 text-3xl font-semibold tracking-tight">
          {t("forgot.title")}
        </h1>
        <div
          className="mb-6 rounded-md border border-amber-200 bg-amber-50 px-4 py-3 text-sm text-amber-900"
          role="alert"
        >
          {t("forgot.passwordDisabled")}
        </div>
        <Link
          to="/login"
          className="text-slate-600 underline hover:text-slate-900"
        >
          {t("forgot.backToLogin")}
        </Link>
      </section>
    );
  }

  return (
    <section className="mx-auto max-w-md px-6 py-16">
      <h1 className="mb-2 text-3xl font-semibold tracking-tight">
        {t("forgot.title")}
      </h1>
      <p className="mb-8 text-slate-600">{t("forgot.subtitle")}</p>

      <form onSubmit={onSubmit} className="space-y-5" noValidate>
        <div>
          <label
            htmlFor="email"
            className="mb-1 block text-sm font-medium text-slate-700"
          >
            {t("forgot.email")}
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
        {mutation.isError && (
          <p
            className="rounded-md bg-red-50 px-3 py-2 text-sm text-red-700"
            role="alert"
          >
            {t(errorKey(mutation.error))}
          </p>
        )}
        <button
          type="submit"
          disabled={mutation.isPending}
          className="inline-flex w-full items-center justify-center rounded-md bg-slate-900 px-4 py-2.5 text-sm font-medium text-white hover:bg-slate-700 disabled:cursor-not-allowed disabled:bg-slate-400"
        >
          {mutation.isPending ? t("forgot.submitting") : t("forgot.submit")}
        </button>
      </form>
    </section>
  );
}
