import { Link, useSearchParams } from 'react-router-dom';
import { useQuery } from '@tanstack/react-query';
import { useTranslation } from 'react-i18next';

import { getMySubscription } from '@/api/subscriptions';

// Stripe Checkout return URL. Cases:
//   result=success → card saved on Stripe; subscription is already
//                    `active` (set at creation time by
//                    SubscriptionService.CreateForTenant). The renewal
//                    job (default 1h cadence) will generate the first
//                    invoice and charge the saved card off-session.
//   result=cancel  → user backed out of Checkout. The subscription
//                    record exists and is `active` regardless — the
//                    user can pay later from the dashboard, or cancel
//                    it via /v1/me/subscriptions/{id}/cancel.
//
// We surface the live subscription so the user can see status straight
// from the source of truth — even on `cancel` returns the call confirms
// the record is real and shows whatever status it currently has.
export function SubscribeReturnPage() {
  const { t } = useTranslation();
  const [params] = useSearchParams();
  const subscriptionUuid = params.get('sub') ?? '';
  const result = params.get('result') === 'cancel' ? 'cancel' : 'success';

  const sub = useQuery({
    queryKey: ['subscription', subscriptionUuid],
    queryFn: ({ signal }) => getMySubscription(subscriptionUuid, signal),
    enabled: subscriptionUuid !== '',
    retry: 2,
  });

  if (!subscriptionUuid) {
    return (
      <section className="mx-auto max-w-2xl px-6 py-16 text-center">
        <h1 className="mb-3 text-3xl font-semibold tracking-tight">
          {t('subscribeReturn.errorTitle')}
        </h1>
        <p className="mb-8 text-slate-600">{t('subscribeReturn.errorMissing')}</p>
        <Link
          to="/account"
          className="inline-flex items-center justify-center rounded-md bg-slate-900 px-4 py-2 text-sm font-medium text-white hover:bg-slate-700"
        >
          {t('subscribeReturn.goAccount')}
        </Link>
      </section>
    );
  }

  if (result === 'cancel') {
    return (
      <section className="mx-auto max-w-2xl px-6 py-16 text-center">
        <h1 className="mb-3 text-3xl font-semibold tracking-tight">
          {t('subscribeReturn.cancelTitle')}
        </h1>
        <p className="mb-8 text-slate-600">{t('subscribeReturn.cancelBody')}</p>
        <div className="flex flex-col items-center gap-3 sm:flex-row sm:justify-center">
          <Link
            to="/catalog"
            className="inline-flex items-center justify-center rounded-md bg-slate-900 px-4 py-2 text-sm font-medium text-white hover:bg-slate-700"
          >
            {t('subscribeReturn.browseCatalog')}
          </Link>
          <Link
            to="/account"
            className="inline-flex items-center justify-center rounded-md border border-slate-300 px-4 py-2 text-sm font-medium text-slate-700 hover:bg-slate-50"
          >
            {t('subscribeReturn.goAccount')}
          </Link>
        </div>
      </section>
    );
  }

  return (
    <section className="mx-auto max-w-2xl px-6 py-16 text-center">
      <h1 className="mb-3 text-3xl font-semibold tracking-tight">
        {t('subscribeReturn.successTitle')}
      </h1>
      <p className="mb-8 text-slate-600">{t('subscribeReturn.successBody')}</p>

      {sub.isLoading && <p className="text-sm text-slate-500">{t('loading')}</p>}
      {sub.isError && (
        <p className="rounded-md bg-amber-50 px-3 py-2 text-sm text-amber-800" role="alert">
          {t('subscribeReturn.warnLookup')}
        </p>
      )}

      {sub.data && (
        <dl className="mx-auto mb-8 grid max-w-sm grid-cols-1 gap-4 rounded-lg border border-slate-200 bg-white p-6 text-left shadow-sm">
          <Field label={t('subscribeReturn.statusLabel')} value={t(`subscribeReturn.status.${sub.data.status}`)} />
          <Field label={t('subscribeReturn.tierLabel')} value={sub.data.tierCode} />
          <Field
            label={t('subscribeReturn.nextBillingLabel')}
            value={
              sub.data.nextBillingAt
                ? new Date(sub.data.nextBillingAt).toLocaleString()
                : '—'
            }
          />
        </dl>
      )}

      <p className="mb-8 text-sm text-slate-500">{t('subscribeReturn.firstChargeNotice')}</p>

      <div className="flex flex-col items-center gap-3 sm:flex-row sm:justify-center">
        <Link
          to="/account"
          className="inline-flex items-center justify-center rounded-md bg-slate-900 px-4 py-2 text-sm font-medium text-white hover:bg-slate-700"
        >
          {t('subscribeReturn.goAccount')}
        </Link>
        <Link
          to="/catalog"
          className="inline-flex items-center justify-center rounded-md border border-slate-300 px-4 py-2 text-sm font-medium text-slate-700 hover:bg-slate-50"
        >
          {t('subscribeReturn.browseCatalog')}
        </Link>
      </div>
    </section>
  );
}

function Field({ label, value }: { label: string; value: string }) {
  return (
    <div>
      <dt className="mb-1 text-xs font-medium uppercase tracking-wider text-slate-500">{label}</dt>
      <dd className="text-base text-slate-900">{value}</dd>
    </div>
  );
}
