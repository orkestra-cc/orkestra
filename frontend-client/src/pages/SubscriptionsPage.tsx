// Phase 6 dashboard — list of every subscription the caller owns,
// across personal + every owned tenant. Backend's MeList fans out across
// owners; the SPA filters that fan-out via the shared OwnerScopeSwitcher
// (private-by-default — switcher only appears when the caller has at
// least one owned tenant).
//
// Service UUIDs are not joined back to a human name on the wire today:
// the backend Subscription model carries serviceUUID without a code or
// label, and the public catalog endpoint lists by code, not UUID. We
// surface the tier code (already human-readable: "monthly", "annual",
// custom labels operators set per service) plus a truncated service id
// so the row stays disambiguating until a future backend pass enriches
// the wire shape.
import { Link } from 'react-router-dom';
import { useMemo } from 'react';
import { useQuery } from '@tanstack/react-query';
import { useTranslation } from 'react-i18next';

import { listMySubscriptions, type Subscription } from '@/api/subscriptions';
import { useMe } from '@/auth/useMe';
import { useOwnerScope, ownerQuery, shortTenantLabel } from '@/auth/ownerScope';
import { OwnerScopeSwitcher } from '@/components/OwnerScopeSwitcher';
import { formatDate } from '@/lib/format';

export function SubscriptionsPage() {
  const { t, i18n } = useTranslation();
  const { data: me } = useMe();
  const { scope, setScope, ownedTenants, hasTenants } = useOwnerScope();
  const filter = useMemo(() => ownerQuery(scope, me?.id), [scope, me?.id]);

  const subs = useQuery({
    queryKey: ['me', 'subscriptions', filter],
    queryFn: ({ signal }) => listMySubscriptions(filter, signal),
  });

  const items = subs.data?.items ?? [];

  return (
    <section className="mx-auto max-w-5xl px-6 py-12">
      <header className="mb-8 flex flex-wrap items-end justify-between gap-4">
        <div>
          <Link to="/account" className="mb-2 inline-block text-sm text-slate-600 hover:underline">
            ← {t('account.back')}
          </Link>
          <h1 className="text-3xl font-semibold tracking-tight">
            {t('dashboard.subscriptions.title')}
          </h1>
          <p className="mt-1 text-slate-600">{t('dashboard.subscriptions.subtitle')}</p>
        </div>
        <div className="flex items-center gap-3">
          <OwnerScopeSwitcher
            scope={scope}
            setScope={setScope}
            ownedTenants={ownedTenants}
            hasTenants={hasTenants}
          />
          <Link
            to="/catalog"
            className="rounded-md bg-slate-900 px-4 py-2 text-sm font-medium text-white hover:bg-slate-700"
          >
            {t('dashboard.subscriptions.startNew')}
          </Link>
        </div>
      </header>

      {subs.isLoading && <p className="text-slate-500">{t('loading')}</p>}
      {subs.isError && (
        <p className="rounded-md bg-red-50 px-3 py-2 text-sm text-red-700" role="alert">
          {t('error.generic')}
        </p>
      )}

      {!subs.isLoading && items.length === 0 && (
        <div className="rounded-lg border border-dashed border-slate-300 bg-white p-10 text-center">
          <p className="mb-4 text-slate-600">{t('dashboard.subscriptions.empty')}</p>
          <Link
            to="/catalog"
            className="inline-flex items-center justify-center rounded-md bg-slate-900 px-4 py-2 text-sm font-medium text-white hover:bg-slate-700"
          >
            {t('dashboard.subscriptions.browseCatalog')}
          </Link>
        </div>
      )}

      {items.length > 0 && (
        <ul className="grid grid-cols-1 gap-4">
          {items.map((sub) => (
            <SubscriptionCard key={sub.uuid} sub={sub} language={i18n.resolvedLanguage ?? 'it'} />
          ))}
        </ul>
      )}
    </section>
  );
}

function SubscriptionCard({ sub, language }: { sub: Subscription; language: string }) {
  const { t } = useTranslation();
  const ownerLabel =
    sub.ownerKind === 'tenant'
      ? t('dashboard.subscriptions.ownerTenant', { id: shortTenantLabel(sub.ownerUUID) })
      : t('dashboard.subscriptions.ownerPersonal');
  const periodEnd = formatDate(sub.currentPeriodEnd, language);
  const nextBilling = sub.nextBillingAt ? formatDate(sub.nextBillingAt, language) : null;

  return (
    <li>
      <Link
        to={`/account/subscriptions/${encodeURIComponent(sub.uuid)}`}
        className="block rounded-lg border border-slate-200 bg-white p-6 shadow-sm transition-shadow hover:border-slate-300 hover:shadow-md"
      >
        <div className="mb-3 flex items-start justify-between gap-4">
          <div>
            <p className="text-xs font-medium uppercase tracking-wider text-slate-500">
              {ownerLabel}
            </p>
            <h2 className="mt-1 text-lg font-semibold text-slate-900">
              {sub.tierCode}
            </h2>
            <p className="mt-1 text-xs text-slate-500">
              {t('dashboard.subscriptions.serviceRef', {
                id: sub.serviceUUID.slice(0, 8),
              })}
            </p>
          </div>
          <StatusBadge status={sub.status} />
        </div>

        <dl className="mt-3 grid grid-cols-1 gap-3 text-sm sm:grid-cols-3">
          <div>
            <dt className="text-xs font-medium uppercase tracking-wider text-slate-500">
              {t('dashboard.subscriptions.periodEnd')}
            </dt>
            <dd className="text-slate-700">{periodEnd}</dd>
          </div>
          {nextBilling && (
            <div>
              <dt className="text-xs font-medium uppercase tracking-wider text-slate-500">
                {t('dashboard.subscriptions.nextBilling')}
              </dt>
              <dd className="text-slate-700">{nextBilling}</dd>
            </div>
          )}
          {sub.cancelAtPeriodEnd && (
            <div>
              <dt className="text-xs font-medium uppercase tracking-wider text-slate-500">
                {t('dashboard.subscriptions.willEnd')}
              </dt>
              <dd className="text-amber-700">{periodEnd}</dd>
            </div>
          )}
        </dl>
      </Link>
    </li>
  );
}

export function StatusBadge({ status }: { status: Subscription['status'] }) {
  const { t } = useTranslation();
  const tone = STATUS_TONE[status];
  return (
    <span
      className={`inline-flex shrink-0 items-center rounded-full px-2.5 py-1 text-xs font-medium ${tone}`}
    >
      {t(`subscribeReturn.status.${status}`)}
    </span>
  );
}

const STATUS_TONE: Record<Subscription['status'], string> = {
  active: 'bg-emerald-50 text-emerald-700',
  past_due: 'bg-amber-50 text-amber-700',
  suspended: 'bg-red-50 text-red-700',
  cancelled: 'bg-slate-100 text-slate-700',
  expired: 'bg-slate-100 text-slate-700',
};
