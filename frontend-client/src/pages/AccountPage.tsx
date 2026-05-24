import { Link, useNavigate } from "react-router-dom";
import { useTranslation } from "react-i18next";

import { useMe } from "@/auth/useMe";
import { useAuth } from "@/auth/useAuth";
import { UserAvatar } from "@/components/UserAvatar";

// Authenticated landing — profile snapshot from /v1/auth/client/me plus
// quick links to security, billing details, and the dashboard surfaces
// (subscriptions / transactions / payment methods). useMe reads from
// the cache the AuthProvider populated on signIn so the page renders
// synchronously after a fresh login.
export function AccountPage() {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const { signOut } = useAuth();
  const { data: me, isLoading, isError } = useMe();

  async function handleSignOut() {
    await signOut();
    navigate("/", { replace: true });
  }

  return (
    <section className="mx-auto max-w-3xl px-6 py-16">
      <header className="mb-10 flex items-start justify-between">
        <div>
          <h1 className="mb-2 text-3xl font-semibold tracking-tight">
            {t("account.title")}
          </h1>
          <p className="text-slate-600">{t("account.subtitle")}</p>
        </div>
        <button
          type="button"
          onClick={handleSignOut}
          className="rounded-md border border-slate-300 px-4 py-2 text-sm font-medium text-slate-700 hover:bg-slate-50"
        >
          {t("nav.signout")}
        </button>
      </header>

      {isLoading && <p className="text-slate-500">{t("loading")}</p>}
      {isError && (
        <p
          className="rounded-md bg-red-50 px-3 py-2 text-sm text-red-700"
          role="alert"
        >
          {t("error.generic")}
        </p>
      )}

      {me && (
        <div className="rounded-lg border border-slate-200 bg-white p-6 shadow-sm">
          <div className="mb-6 flex items-center gap-4 border-b border-slate-100 pb-6">
            <UserAvatar user={me} size="xl" />
            <div>
              <div className="text-lg font-semibold text-slate-900">
                {me.fullName || me.email}
              </div>
              <div className="text-sm text-slate-600">{me.email}</div>
            </div>
          </div>
          <dl className="grid grid-cols-1 gap-6 sm:grid-cols-2">
            <Field label={t("account.fullName")} value={me.fullName ?? "—"} />
            <Field label={t("account.email")} value={me.email} />
            <Field
              label={t("account.emailVerified")}
              value={
                me.emailVerified
                  ? t("account.verified")
                  : t("account.notVerified")
              }
            />
            <Field label={t("account.role")} value={me.role ?? "—"} />
          </dl>
        </div>
      )}

      <div className="mt-8 grid grid-cols-1 gap-4 sm:grid-cols-2">
        <Link
          to="/account/profile"
          className="rounded-lg border border-slate-200 bg-white p-6 shadow-sm transition-shadow hover:border-slate-300 hover:shadow-md"
        >
          <h2 className="mb-1 text-base font-semibold text-slate-900">
            {t("account.profile.title")}
          </h2>
          <p className="text-sm text-slate-600">{t("account.profile.cta")}</p>
        </Link>
        <Link
          to="/account/security"
          className="rounded-lg border border-slate-200 bg-white p-6 shadow-sm transition-shadow hover:border-slate-300 hover:shadow-md"
        >
          <h2 className="mb-1 text-base font-semibold text-slate-900">
            {t("account.security.title")}
          </h2>
          <p className="text-sm text-slate-600">{t("account.security.cta")}</p>
        </Link>
        <Link
          to="/account/billing"
          className="rounded-lg border border-slate-200 bg-white p-6 shadow-sm transition-shadow hover:border-slate-300 hover:shadow-md"
        >
          <h2 className="mb-1 text-base font-semibold text-slate-900">
            {t("account.billing.title")}
          </h2>
          <p className="text-sm text-slate-600">{t("account.billing.cta")}</p>
        </Link>
        <Link
          to="/account/subscriptions"
          className="rounded-lg border border-slate-200 bg-white p-6 shadow-sm transition-shadow hover:border-slate-300 hover:shadow-md"
        >
          <h2 className="mb-1 text-base font-semibold text-slate-900">
            {t("account.subscriptions.title")}
          </h2>
          <p className="text-sm text-slate-600">
            {t("account.subscriptions.cta")}
          </p>
        </Link>
        <Link
          to="/account/transactions"
          className="rounded-lg border border-slate-200 bg-white p-6 shadow-sm transition-shadow hover:border-slate-300 hover:shadow-md"
        >
          <h2 className="mb-1 text-base font-semibold text-slate-900">
            {t("account.transactions.title")}
          </h2>
          <p className="text-sm text-slate-600">
            {t("account.transactions.cta")}
          </p>
        </Link>
        <Link
          to="/account/payment-methods"
          className="rounded-lg border border-slate-200 bg-white p-6 shadow-sm transition-shadow hover:border-slate-300 hover:shadow-md"
        >
          <h2 className="mb-1 text-base font-semibold text-slate-900">
            {t("account.paymentMethods.title")}
          </h2>
          <p className="text-sm text-slate-600">
            {t("account.paymentMethods.cta")}
          </p>
        </Link>
      </div>
    </section>
  );
}

function Field({ label, value }: { label: string; value: string }) {
  return (
    <div>
      <dt className="mb-1 text-xs font-medium uppercase tracking-wider text-slate-500">
        {label}
      </dt>
      <dd className="text-base text-slate-900">{value}</dd>
    </div>
  );
}
