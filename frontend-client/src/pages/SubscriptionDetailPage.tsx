// Phase 6 dashboard — subscription detail. Three sections:
//   1. Header with status, owner, period, cancel/reactivate buttons.
//   2. Invoices table — pay-now (payment-mode Checkout) on pending or
//      failed rows.
//   3. Activity timeline — append-only audit feed from
//      /v1/me/subscriptions/{id}/activity.
//
// Cancel + reactivate hit the matching /v1/me/subscriptions/{id}/cancel|
// reactivate endpoints; both invalidate the subscription query so the
// header status flips synchronously after the mutation resolves.
import { Link, useNavigate, useParams, useSearchParams } from 'react-router-dom';
import { useMemo, useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { useTranslation } from 'react-i18next';

import {
  cancelMySubscription,
  getMySubscription,
  listMyActivity,
  listMyInvoices,
  reactivateMySubscription,
  type ActivityLog,
  type Subscription,
  type SubscriptionInvoice,
} from '@/api/subscriptions';
import {
  createPaymentCheckoutSession,
  type PaymentApiError,
} from '@/api/payments';
import { formatDate, formatDateTime, formatPrice } from '@/lib/format';
import { shortTenantLabel } from '@/auth/ownerScope';
import { StatusBadge } from '@/pages/SubscriptionsPage';

export function SubscriptionDetailPage() {
  const { t, i18n } = useTranslation();
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const params = useParams<{ id: string }>();
  const [search] = useSearchParams();
  const paid = search.get('paid');
  const id = params.id ?? '';
  const language = i18n.resolvedLanguage ?? 'it';

  const subQ = useQuery({
    queryKey: ['me', 'subscription', id],
    queryFn: ({ signal }) => getMySubscription(id, signal),
    enabled: !!id,
  });
  const invoicesQ = useQuery({
    queryKey: ['me', 'subscription', id, 'invoices'],
    queryFn: ({ signal }) => listMyInvoices(id, signal),
    enabled: !!id,
  });
  const activityQ = useQuery({
    queryKey: ['me', 'subscription', id, 'activity'],
    queryFn: ({ signal }) => listMyActivity(id, 100, signal),
    enabled: !!id,
  });

  const sub = subQ.data;

  const cancelMutation = useMutation({
    mutationFn: (atPeriodEnd: boolean) => cancelMySubscription(id, atPeriodEnd),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['me', 'subscription', id] });
      queryClient.invalidateQueries({ queryKey: ['me', 'subscriptions'] });
      queryClient.invalidateQueries({ queryKey: ['me', 'subscription', id, 'activity'] });
    },
  });
  const reactivateMutation = useMutation({
    mutationFn: () => reactivateMySubscription(id),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['me', 'subscription', id] });
      queryClient.invalidateQueries({ queryKey: ['me', 'subscriptions'] });
      queryClient.invalidateQueries({ queryKey: ['me', 'subscription', id, 'activity'] });
    },
  });
  const payMutation = useMutation<unknown, Error, void>({
    mutationFn: async () => {
      const origin = window.location.origin;
      const successUrl = `${origin}/account/subscriptions/${encodeURIComponent(id)}?paid=success`;
      const cancelUrl = `${origin}/account/subscriptions/${encodeURIComponent(id)}?paid=cancel`;
      const session = await createPaymentCheckoutSession({
        subscriptionUuid: id,
        successUrl,
        cancelUrl,
      });
      window.location.href = session.url;
    },
    onError: (e) => {
      const apiErr = e as PaymentApiError;
      // 409: backend has no pending invoice — surface a friendly toast in
      // place of the raw error. The renewal job ticks ~hourly; the user
      // should see a pending row appear after the next cycle.
      if (apiErr.status === 409) {
        return;
      }
    },
  });

  if (!id) {
    return (
      <section className="mx-auto max-w-3xl px-6 py-16 text-center">
        <h1 className="mb-3 text-3xl font-semibold tracking-tight">
          {t('dashboard.subscriptionDetail.errorMissing')}
        </h1>
        <button
          type="button"
          onClick={() => navigate('/account/subscriptions')}
          className="text-sm text-slate-600 hover:underline"
        >
          {t('dashboard.subscriptions.title')}
        </button>
      </section>
    );
  }

  if (subQ.isLoading) {
    return (
      <section className="mx-auto max-w-4xl px-6 py-12">
        <p className="text-slate-500">{t('loading')}</p>
      </section>
    );
  }

  if (subQ.isError || !sub) {
    return (
      <section className="mx-auto max-w-4xl px-6 py-12">
        <Link
          to="/account/subscriptions"
          className="mb-6 inline-block text-sm text-slate-600 hover:underline"
        >
          ← {t('dashboard.subscriptions.title')}
        </Link>
        <p className="rounded-md bg-red-50 px-3 py-2 text-sm text-red-700" role="alert">
          {t('dashboard.subscriptionDetail.errorLoad')}
        </p>
      </section>
    );
  }

  const ownerLabel =
    sub.ownerKind === 'tenant'
      ? t('dashboard.subscriptions.ownerTenant', { id: shortTenantLabel(sub.ownerUUID) })
      : t('dashboard.subscriptions.ownerPersonal');

  return (
    <section className="mx-auto max-w-4xl px-6 py-12">
      <Link
        to="/account/subscriptions"
        className="mb-6 inline-block text-sm text-slate-600 hover:underline"
      >
        ← {t('dashboard.subscriptions.title')}
      </Link>

      <header className="mb-8 flex flex-wrap items-start justify-between gap-4 rounded-lg border border-slate-200 bg-white p-6 shadow-sm">
        <div>
          <p className="text-xs font-medium uppercase tracking-wider text-slate-500">
            {ownerLabel}
          </p>
          <h1 className="mt-1 text-2xl font-semibold tracking-tight">{sub.tierCode}</h1>
          <p className="mt-1 text-xs text-slate-500">
            {t('dashboard.subscriptions.serviceRef', { id: sub.serviceUUID.slice(0, 8) })}
          </p>
          <dl className="mt-4 grid grid-cols-1 gap-3 text-sm sm:grid-cols-3">
            <DefField
              label={t('dashboard.subscriptions.periodStart')}
              value={formatDate(sub.currentPeriodStart, language)}
            />
            <DefField
              label={t('dashboard.subscriptions.periodEnd')}
              value={formatDate(sub.currentPeriodEnd, language)}
            />
            {sub.nextBillingAt && (
              <DefField
                label={t('dashboard.subscriptions.nextBilling')}
                value={formatDate(sub.nextBillingAt, language)}
              />
            )}
            {sub.cancelAtPeriodEnd && (
              <DefField
                label={t('dashboard.subscriptions.willEnd')}
                value={formatDate(sub.currentPeriodEnd, language)}
                tone="amber"
              />
            )}
          </dl>
        </div>
        <div className="flex flex-col items-end gap-3">
          <StatusBadge status={sub.status} />
          <SubscriptionActions
            sub={sub}
            onCancel={(atPeriodEnd) => cancelMutation.mutate(atPeriodEnd)}
            onReactivate={() => reactivateMutation.mutate()}
            cancelPending={cancelMutation.isPending}
            reactivatePending={reactivateMutation.isPending}
          />
        </div>
      </header>

      {paid === 'success' && (
        <p className="mb-4 rounded-md bg-emerald-50 px-3 py-2 text-sm text-emerald-800" role="status">
          {t('dashboard.subscriptionDetail.paySuccess')}
        </p>
      )}
      {paid === 'cancel' && (
        <p className="mb-4 rounded-md bg-slate-100 px-3 py-2 text-sm text-slate-700" role="status">
          {t('dashboard.subscriptionDetail.payCancel')}
        </p>
      )}

      {(cancelMutation.isError || reactivateMutation.isError) && (
        <p className="mb-4 rounded-md bg-red-50 px-3 py-2 text-sm text-red-700" role="alert">
          {(cancelMutation.error ?? reactivateMutation.error)?.message ?? t('error.generic')}
        </p>
      )}
      {payMutation.isError && (
        <p className="mb-4 rounded-md bg-amber-50 px-3 py-2 text-sm text-amber-700" role="alert">
          {(payMutation.error as PaymentApiError).status === 409
            ? t('dashboard.subscriptionDetail.payNoPending')
            : payMutation.error.message}
        </p>
      )}

      <SectionHeading>{t('dashboard.subscriptionDetail.invoicesTitle')}</SectionHeading>
      <InvoicesTable
        invoices={invoicesQ.data?.items ?? []}
        loading={invoicesQ.isLoading}
        error={invoicesQ.isError}
        language={language}
        onPay={() => payMutation.mutate()}
        payPending={payMutation.isPending}
      />

      <SectionHeading className="mt-10">
        {t('dashboard.subscriptionDetail.activityTitle')}
      </SectionHeading>
      <ActivityTimeline
        items={activityQ.data?.items ?? []}
        loading={activityQ.isLoading}
        error={activityQ.isError}
        language={language}
      />
    </section>
  );
}

function SectionHeading({
  children,
  className = '',
}: {
  children: React.ReactNode;
  className?: string;
}) {
  return <h2 className={`mb-3 text-lg font-semibold text-slate-900 ${className}`}>{children}</h2>;
}

function DefField({ label, value, tone }: { label: string; value: string; tone?: 'amber' }) {
  return (
    <div>
      <dt className="text-xs font-medium uppercase tracking-wider text-slate-500">{label}</dt>
      <dd className={tone === 'amber' ? 'text-amber-700' : 'text-slate-700'}>{value}</dd>
    </div>
  );
}

function SubscriptionActions({
  sub,
  onCancel,
  onReactivate,
  cancelPending,
  reactivatePending,
}: {
  sub: Subscription;
  onCancel: (atPeriodEnd: boolean) => void;
  onReactivate: () => void;
  cancelPending: boolean;
  reactivatePending: boolean;
}) {
  const { t } = useTranslation();
  const [confirming, setConfirming] = useState<'cancel' | null>(null);

  const canCancel = sub.status === 'active' || sub.status === 'past_due';
  const canReactivate = sub.cancelAtPeriodEnd && sub.status !== 'cancelled';

  if (canReactivate) {
    return (
      <button
        type="button"
        onClick={onReactivate}
        disabled={reactivatePending}
        className="rounded-md border border-emerald-300 bg-white px-4 py-2 text-sm font-medium text-emerald-700 hover:bg-emerald-50 disabled:cursor-not-allowed disabled:opacity-60"
      >
        {reactivatePending
          ? t('dashboard.subscriptionDetail.reactivating')
          : t('dashboard.subscriptionDetail.reactivate')}
      </button>
    );
  }

  if (!canCancel) return null;

  if (confirming !== 'cancel') {
    return (
      <button
        type="button"
        onClick={() => setConfirming('cancel')}
        className="rounded-md border border-slate-300 bg-white px-4 py-2 text-sm font-medium text-slate-700 hover:bg-slate-50"
      >
        {t('dashboard.subscriptionDetail.cancel')}
      </button>
    );
  }

  return (
    <div className="flex flex-col gap-2 rounded-md border border-amber-200 bg-amber-50 p-3 text-sm">
      <p className="text-amber-800">{t('dashboard.subscriptionDetail.cancelPrompt')}</p>
      <div className="flex flex-wrap gap-2">
        <button
          type="button"
          onClick={() => onCancel(true)}
          disabled={cancelPending}
          className="rounded-md bg-amber-600 px-3 py-1.5 text-xs font-medium text-white hover:bg-amber-700 disabled:cursor-not-allowed disabled:opacity-60"
        >
          {t('dashboard.subscriptionDetail.cancelAtPeriodEnd')}
        </button>
        <button
          type="button"
          onClick={() => onCancel(false)}
          disabled={cancelPending}
          className="rounded-md bg-red-600 px-3 py-1.5 text-xs font-medium text-white hover:bg-red-700 disabled:cursor-not-allowed disabled:opacity-60"
        >
          {t('dashboard.subscriptionDetail.cancelImmediately')}
        </button>
        <button
          type="button"
          onClick={() => setConfirming(null)}
          className="rounded-md px-3 py-1.5 text-xs font-medium text-slate-600 hover:text-slate-900"
        >
          {t('dashboard.subscriptionDetail.cancelKeep')}
        </button>
      </div>
    </div>
  );
}

function InvoicesTable({
  invoices,
  loading,
  error,
  language,
  onPay,
  payPending,
}: {
  invoices: SubscriptionInvoice[];
  loading: boolean;
  error: boolean;
  language: string;
  onPay: () => void;
  payPending: boolean;
}) {
  const { t } = useTranslation();

  if (loading) return <p className="text-slate-500">{t('loading')}</p>;
  if (error)
    return (
      <p className="rounded-md bg-red-50 px-3 py-2 text-sm text-red-700" role="alert">
        {t('dashboard.subscriptionDetail.invoicesError')}
      </p>
    );
  if (invoices.length === 0)
    return (
      <p className="rounded-md border border-dashed border-slate-300 bg-white p-4 text-sm text-slate-500">
        {t('dashboard.subscriptionDetail.invoicesEmpty')}
      </p>
    );

  // Backend can include several "pending" invoices (renewal job missed
  // ticks, manual generation). Show pay-now on the top-most pending or
  // failed row to avoid double-charging across rows.
  const payableIndex = invoices.findIndex(
    (inv) => inv.status === 'pending' || inv.status === 'failed',
  );

  return (
    <div className="overflow-x-auto rounded-lg border border-slate-200 bg-white shadow-sm">
      <table className="min-w-full divide-y divide-slate-200 text-sm">
        <thead className="bg-slate-50 text-xs uppercase tracking-wider text-slate-500">
          <tr>
            <Th>{t('dashboard.subscriptionDetail.invoiceNumber')}</Th>
            <Th>{t('dashboard.subscriptionDetail.invoicePeriod')}</Th>
            <Th>{t('dashboard.subscriptionDetail.invoiceAmount')}</Th>
            <Th>{t('dashboard.subscriptionDetail.invoiceStatus')}</Th>
            <Th>{t('dashboard.subscriptionDetail.invoiceIssued')}</Th>
            <Th />
          </tr>
        </thead>
        <tbody className="divide-y divide-slate-100">
          {invoices.map((inv, i) => (
            <tr key={inv.uuid}>
              <Td>{inv.number}</Td>
              <Td>
                {formatDate(inv.periodStart, language)} → {formatDate(inv.periodEnd, language)}
              </Td>
              <Td>{formatPrice(inv.totalCents, inv.currency, language)}</Td>
              <Td>
                <InvoiceStatusBadge status={inv.status} />
              </Td>
              <Td>{formatDate(inv.issuedAt, language)}</Td>
              <Td>
                {i === payableIndex && (
                  <button
                    type="button"
                    onClick={onPay}
                    disabled={payPending}
                    className="rounded-md bg-slate-900 px-3 py-1.5 text-xs font-medium text-white hover:bg-slate-700 disabled:cursor-not-allowed disabled:opacity-60"
                  >
                    {payPending
                      ? t('dashboard.subscriptionDetail.paying')
                      : t('dashboard.subscriptionDetail.payNow')}
                  </button>
                )}
              </Td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}

function ActivityTimeline({
  items,
  loading,
  error,
  language,
}: {
  items: ActivityLog[];
  loading: boolean;
  error: boolean;
  language: string;
}) {
  const { t } = useTranslation();
  const sorted = useMemo(
    () =>
      [...items].sort((a, b) => new Date(b.createdAt).getTime() - new Date(a.createdAt).getTime()),
    [items],
  );

  if (loading) return <p className="text-slate-500">{t('loading')}</p>;
  if (error)
    return (
      <p className="rounded-md bg-red-50 px-3 py-2 text-sm text-red-700" role="alert">
        {t('dashboard.subscriptionDetail.activityError')}
      </p>
    );
  if (sorted.length === 0)
    return (
      <p className="rounded-md border border-dashed border-slate-300 bg-white p-4 text-sm text-slate-500">
        {t('dashboard.subscriptionDetail.activityEmpty')}
      </p>
    );

  return (
    <ol className="space-y-3">
      {sorted.map((a) => (
        <li
          key={a.uuid}
          className="flex items-start gap-3 rounded-lg border border-slate-200 bg-white p-4 shadow-sm"
        >
          <div className="mt-0.5 h-2 w-2 shrink-0 rounded-full bg-slate-400" aria-hidden />
          <div className="min-w-0 flex-1">
            <div className="flex flex-wrap items-baseline justify-between gap-2">
              <span className="text-sm font-medium text-slate-900">
                {t(`dashboard.activity.type.${a.type}`, { defaultValue: a.type })}
              </span>
              <time className="text-xs text-slate-500" dateTime={a.createdAt}>
                {formatDateTime(a.createdAt, language)}
              </time>
            </div>
            {a.message && <p className="mt-1 text-sm text-slate-600">{a.message}</p>}
            <p className="mt-1 text-xs text-slate-400">
              {t('dashboard.activity.actor', { actor: a.actor })}
            </p>
          </div>
        </li>
      ))}
    </ol>
  );
}

function InvoiceStatusBadge({ status }: { status: SubscriptionInvoice['status'] }) {
  const { t } = useTranslation();
  const tone = INVOICE_TONE[status] ?? 'bg-slate-100 text-slate-700';
  return (
    <span
      className={`inline-flex items-center rounded-full px-2 py-0.5 text-xs font-medium ${tone}`}
    >
      {t(`dashboard.subscriptionDetail.invoiceStatusValue.${status}`, { defaultValue: status })}
    </span>
  );
}

const INVOICE_TONE: Record<SubscriptionInvoice['status'], string> = {
  pending: 'bg-amber-50 text-amber-700',
  paid: 'bg-emerald-50 text-emerald-700',
  failed: 'bg-red-50 text-red-700',
  refunded: 'bg-slate-100 text-slate-700',
  void: 'bg-slate-100 text-slate-500',
  awaiting_manual_payment: 'bg-blue-50 text-blue-700',
};

function Th({ children }: { children?: React.ReactNode }) {
  return <th className="px-4 py-3 text-left font-medium">{children}</th>;
}

function Td({ children }: { children?: React.ReactNode }) {
  return <td className="px-4 py-3 align-middle text-slate-700">{children}</td>;
}

