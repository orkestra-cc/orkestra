// Phase 6 dashboard — saved payment methods. Backend's MeListPaymentMethods
// fans out across every owner; the SPA narrows via OwnerScopeSwitcher.
//
// "Add card" opens setup-mode Stripe Checkout against the active scope.
// When scope is "all" (the default fan-out) we fall back to the calling
// user — the most common Tier-2 case. The webhook reconciler attaches
// the resulting pm_xxx token to the matching owner via the metadata
// stamp the setup-checkout endpoint places on the SetupIntent.
import { Link } from 'react-router-dom';
import { useMemo } from 'react';
import { useMutation, useQuery } from '@tanstack/react-query';
import { useTranslation } from 'react-i18next';

import {
  createSetupCheckoutSession,
  listMyPaymentMethods,
  type CreateSetupCheckoutInput,
  type PaymentMethod,
} from '@/api/payments';
import { useMe } from '@/auth/useMe';
import { useOwnerScope, ownerQuery, shortTenantLabel } from '@/auth/ownerScope';
import { OwnerScopeSwitcher } from '@/components/OwnerScopeSwitcher';

export function PaymentMethodsPage() {
  const { t } = useTranslation();
  const { data: me } = useMe();
  const { scope, setScope, ownedTenants, hasTenants } = useOwnerScope();
  const filter = useMemo(() => ownerQuery(scope, me?.id), [scope, me?.id]);

  const methods = useQuery({
    queryKey: ['me', 'payment-methods', filter],
    queryFn: ({ signal }) => listMyPaymentMethods(filter, signal),
  });

  const addCardMutation = useMutation({
    mutationFn: async () => {
      const origin = window.location.origin;
      const successUrl = `${origin}/account/payment-methods?added=success`;
      const cancelUrl = `${origin}/account/payment-methods?added=cancel`;
      // Resolve the owner for the new card. Default to the calling user
      // when scope is "all" — the most common Tier-2 case is a personal
      // self-registered client. Tenant-scope routes the card to the
      // selected owned org.
      const input: CreateSetupCheckoutInput = { successUrl, cancelUrl };
      if (scope.kind === 'tenant') {
        input.ownerKind = 'tenant';
        input.ownerUuid = scope.uuid;
      } else {
        // Both 'all' and 'user' route to the calling user's profile.
        input.ownerKind = 'user';
        if (me?.id) input.ownerUuid = me.id;
      }
      const session = await createSetupCheckoutSession(input);
      window.location.href = session.url;
    },
  });

  const items = useMemo(() => {
    const list = methods.data?.items ?? [];
    return [...list].sort((a, b) => Number(b.isDefault) - Number(a.isDefault));
  }, [methods.data]);

  return (
    <section className="mx-auto max-w-4xl px-6 py-12">
      <header className="mb-8 flex flex-wrap items-end justify-between gap-4">
        <div>
          <Link to="/account" className="mb-2 inline-block text-sm text-slate-600 hover:underline">
            ← {t('account.back')}
          </Link>
          <h1 className="text-3xl font-semibold tracking-tight">
            {t('dashboard.paymentMethods.title')}
          </h1>
          <p className="mt-1 text-slate-600">{t('dashboard.paymentMethods.subtitle')}</p>
        </div>
        <div className="flex items-center gap-3">
          <OwnerScopeSwitcher
            scope={scope}
            setScope={setScope}
            ownedTenants={ownedTenants}
            hasTenants={hasTenants}
          />
          <button
            type="button"
            onClick={() => addCardMutation.mutate()}
            disabled={addCardMutation.isPending}
            className="rounded-md bg-slate-900 px-4 py-2 text-sm font-medium text-white hover:bg-slate-700 disabled:cursor-not-allowed disabled:opacity-60"
          >
            {addCardMutation.isPending
              ? t('dashboard.paymentMethods.adding')
              : t('dashboard.paymentMethods.addCard')}
          </button>
        </div>
      </header>

      {addCardMutation.isError && (
        <p className="mb-4 rounded-md bg-red-50 px-3 py-2 text-sm text-red-700" role="alert">
          {addCardMutation.error.message}
        </p>
      )}

      {methods.isLoading && <p className="text-slate-500">{t('loading')}</p>}
      {methods.isError && (
        <p className="rounded-md bg-red-50 px-3 py-2 text-sm text-red-700" role="alert">
          {t('error.generic')}
        </p>
      )}

      {!methods.isLoading && items.length === 0 && (
        <p className="rounded-lg border border-dashed border-slate-300 bg-white p-10 text-center text-slate-600">
          {t('dashboard.paymentMethods.empty')}
        </p>
      )}

      {items.length > 0 && (
        <ul className="grid grid-cols-1 gap-4 sm:grid-cols-2">
          {items.map((m) => (
            <PaymentMethodCard key={m.uuid} method={m} />
          ))}
        </ul>
      )}
    </section>
  );
}

function PaymentMethodCard({ method }: { method: PaymentMethod }) {
  const { t } = useTranslation();
  const ownerLabel =
    method.ownerKind === 'tenant'
      ? t('dashboard.subscriptions.ownerTenant', { id: shortTenantLabel(method.ownerUUID) })
      : t('dashboard.subscriptions.ownerPersonal');
  const expiry =
    method.expiryMonth && method.expiryYear
      ? `${String(method.expiryMonth).padStart(2, '0')}/${String(method.expiryYear).slice(-2)}`
      : null;

  return (
    <li className="rounded-lg border border-slate-200 bg-white p-5 shadow-sm">
      <div className="flex items-start justify-between gap-3">
        <div>
          <p className="text-xs font-medium uppercase tracking-wider text-slate-500">{ownerLabel}</p>
          <p className="mt-1 text-base font-semibold capitalize text-slate-900">
            {method.brand || method.provider}
          </p>
          {method.last4 && (
            <p className="mt-1 font-mono text-sm text-slate-700">•••• {method.last4}</p>
          )}
        </div>
        {method.isDefault && (
          <span className="inline-flex items-center rounded-full bg-emerald-50 px-2.5 py-1 text-xs font-medium text-emerald-700">
            {t('dashboard.paymentMethods.defaultBadge')}
          </span>
        )}
      </div>
      {expiry && (
        <p className="mt-3 text-xs text-slate-500">
          {t('dashboard.paymentMethods.expires', { date: expiry })}
        </p>
      )}
    </li>
  );
}
