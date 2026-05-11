import { useEffect, useMemo, useState } from 'react';
import { Link, useNavigate, useSearchParams } from 'react-router-dom';
import { useMutation, useQuery } from '@tanstack/react-query';
import { useTranslation } from 'react-i18next';

import { listPublicCatalog, findServiceByCode } from '@/api/catalog';
import {
  selfSubscribe,
  type SelfSubscribeInput,
  type Subscription,
} from '@/api/subscriptions';
import {
  createSetupCheckoutSession,
  type CreateSetupCheckoutInput,
  type PaymentApiError,
} from '@/api/payments';
import { getBillingProfile, hasBillingProfile } from '@/api/billingProfile';
import { useOwnedTenants } from '@/auth/memberships';
import { formatPrice } from '@/lib/format';

// Subscribe orchestration page (Phase 3 polymorphic-owner update):
//
//   1. Read service+tier from query string (set by /catalog/:code).
//   2. Resolve the owner. Default is "personal" — owner = the calling
//      user, no tenant required. If the caller owns one or more tenants
//      they pick "Personal" or one of those tenants from a dropdown.
//      The backend re-checks ownership on every /v1/me/* call so this is
//      a UX hint, not a security gate.
//   3. For user-owner subscribes the caller must have a billing profile
//      (Phase 2 of the polymorphic-owner refactor). We GET it first; if
//      missing, redirect to /account/billing?next=/subscribe?... so the
//      flow returns here once the form is saved. Tenant-owner subscribes
//      reuse the existing tenant.StripeCustomerID seam — no profile
//      check needed.
//   4. POST /v1/me/subscriptions — backend creates the subscription with
//      Status=active and NextBillingAt=now (entitlements granted
//      synchronously by the entitlement syncer).
//   5. POST /v1/me/payments/setup-checkout-session — opens hosted Stripe
//      Checkout in setup mode so the card is saved without charging. The
//      renewal job (default 1h cadence) generates the first invoice and
//      charges the saved card off-session via the existing webhook
//      reconciler metadata stamp. If the backend returns 409 ("complete
//      your billing profile") we still bounce to the billing form — the
//      pre-flight GET should make this rare but not impossible (e.g. a
//      mid-flow profile reset).
//   6. Redirect to Stripe via window.location.href. Return URL is
//      /subscribe/return?sub={uuid}&result=success|cancel.
//
// Design note: payment-mode Checkout requires a pending invoice to
// already exist (planner returns ErrCheckoutNoPendingInvoice → 409).
// At cold-subscribe time no invoice exists yet, so payment-mode is
// not viable here — it's the right tool for paying outstanding
// invoices from the subscription detail page. Setup mode covers the
// cold path without backend changes.
const PERSONAL_OPTION = '__personal__';

export function SubscribePage() {
  const { t, i18n } = useTranslation();
  const navigate = useNavigate();
  const [params] = useSearchParams();
  const serviceCode = params.get('service') ?? '';
  const tierCode = params.get('tier') ?? '';

  const ownedTenants = useOwnedTenants();
  // Default the owner to "personal" — every authenticated client can
  // subscribe as themselves regardless of tenant memberships.
  const [selectedOwner, setSelectedOwner] = useState<string>(PERSONAL_OPTION);
  const [profileGate, setProfileGate] = useState<'idle' | 'checking'>('idle');

  const catalog = useQuery({
    queryKey: ['catalog'],
    queryFn: ({ signal }) => listPublicCatalog(signal),
    staleTime: 60_000,
  });
  const service = useMemo(
    () => findServiceByCode(catalog.data?.items, serviceCode),
    [catalog.data, serviceCode],
  );
  const tier = service?.pricingTiers.find((t) => t.code === tierCode);
  const language = i18n.resolvedLanguage ?? 'it';

  // If the user has no owned tenants the dropdown is hidden and the
  // owner stays "personal". Reset to personal whenever the membership
  // set changes so a stale tenant id doesn't linger after a token
  // rotation removes ownership.
  useEffect(() => {
    if (selectedOwner === PERSONAL_OPTION) return;
    const stillOwned = ownedTenants.some((m) => m.tenantUuid === selectedOwner);
    if (!stillOwned) setSelectedOwner(PERSONAL_OPTION);
  }, [ownedTenants, selectedOwner]);

  const subscribeMutation = useMutation<
    { sub: Subscription; checkoutUrl: string },
    Error,
    { ownerKind: 'user' | 'tenant'; tenantUuid?: string }
  >({
    mutationFn: async ({ ownerKind, tenantUuid }) => {
      if (!serviceCode || !tierCode) throw new Error(t('subscribe.errorMissingTier'));
      const subInput: SelfSubscribeInput = { serviceCode, tierCode, ownerKind };
      if (ownerKind === 'tenant') subInput.tenantUuid = tenantUuid;
      const sub = await selfSubscribe(subInput);
      const origin = window.location.origin;
      const successUrl = `${origin}/subscribe/return?sub=${encodeURIComponent(sub.uuid)}&result=success`;
      const cancelUrl = `${origin}/subscribe/return?sub=${encodeURIComponent(sub.uuid)}&result=cancel`;
      const checkoutInput: CreateSetupCheckoutInput = {
        ownerKind,
        successUrl,
        cancelUrl,
      };
      if (ownerKind === 'tenant') checkoutInput.tenantUuid = tenantUuid;
      const session = await createSetupCheckoutSession(checkoutInput);
      return { sub, checkoutUrl: session.url };
    },
    onSuccess: ({ checkoutUrl }) => {
      // Stripe-hosted Checkout — full-page navigation. The browser
      // returns to /subscribe/return after the user enters card details
      // (success) or hits cancel.
      window.location.href = checkoutUrl;
    },
    onError: (e) => {
      const apiErr = e as PaymentApiError;
      // Backend signals "user has not filled billing profile" with 409.
      // The pre-flight GET below should make this rare, but a stale
      // profile (e.g. cleared between tabs) would still land here.
      if (apiErr.status === 409) {
        const here = `/subscribe?service=${encodeURIComponent(serviceCode)}&tier=${encodeURIComponent(tierCode)}`;
        navigate(`/account/billing?next=${encodeURIComponent(here)}`);
      }
    },
  });

  async function handleSubscribe() {
    if (subscribeMutation.isPending || profileGate === 'checking') return;
    if (selectedOwner === PERSONAL_OPTION) {
      // Pre-flight check: only the user-owner branch needs a billing
      // profile. Tenant-owner uses the tenant's existing details.
      setProfileGate('checking');
      try {
        const profile = await getBillingProfile();
        if (!hasBillingProfile(profile)) {
          const here = `/subscribe?service=${encodeURIComponent(serviceCode)}&tier=${encodeURIComponent(tierCode)}`;
          navigate(`/account/billing?next=${encodeURIComponent(here)}`);
          return;
        }
      } finally {
        setProfileGate('idle');
      }
      subscribeMutation.mutate({ ownerKind: 'user' });
      return;
    }
    subscribeMutation.mutate({ ownerKind: 'tenant', tenantUuid: selectedOwner });
  }

  if (!serviceCode || !tierCode) {
    return (
      <section className="mx-auto max-w-2xl px-6 py-16 text-center">
        <h1 className="mb-3 text-3xl font-semibold tracking-tight">
          {t('subscribe.errorTitle')}
        </h1>
        <p className="mb-8 text-slate-600">{t('subscribe.errorMissingTier')}</p>
        <Link
          to="/catalog"
          className="inline-flex items-center justify-center rounded-md bg-slate-900 px-4 py-2 text-sm font-medium text-white hover:bg-slate-700"
        >
          {t('subscribe.browseCatalog')}
        </Link>
      </section>
    );
  }

  if (catalog.isLoading) {
    return (
      <section className="mx-auto max-w-2xl px-6 py-16">
        <p className="text-slate-500">{t('loading')}</p>
      </section>
    );
  }

  if (!service || !tier) {
    return (
      <section className="mx-auto max-w-2xl px-6 py-16 text-center">
        <h1 className="mb-3 text-3xl font-semibold tracking-tight">
          {t('subscribe.errorTitle')}
        </h1>
        <p className="mb-8 text-slate-600">{t('subscribe.errorTierGone')}</p>
        <Link
          to="/catalog"
          className="inline-flex items-center justify-center rounded-md bg-slate-900 px-4 py-2 text-sm font-medium text-white hover:bg-slate-700"
        >
          {t('subscribe.browseCatalog')}
        </Link>
      </section>
    );
  }

  const price = formatPrice(tier.amountCents, tier.currency, language);
  const setupFee = service.setupFeeCents
    ? formatPrice(service.setupFeeCents, tier.currency, language)
    : null;

  return (
    <section className="mx-auto max-w-xl px-6 py-16">
      <Link to={`/catalog/${encodeURIComponent(serviceCode)}`} className="mb-8 inline-block text-sm text-slate-600 hover:underline">
        ← {t('subscribe.back')}
      </Link>

      <h1 className="mb-2 text-3xl font-semibold tracking-tight">{t('subscribe.title')}</h1>
      <p className="mb-10 text-slate-600">{t('subscribe.subtitle')}</p>

      <div className="mb-8 rounded-lg border border-slate-200 bg-white p-6 shadow-sm">
        <p className="mb-1 text-xs font-medium uppercase tracking-wider text-slate-500">
          {service.category ?? t('subscribe.servicePlan')}
        </p>
        <h2 className="mb-1 text-xl font-semibold text-slate-900">{service.name}</h2>
        <p className="mb-4 text-sm text-slate-600">{tier.code}</p>
        <p className="text-2xl font-semibold tracking-tight text-slate-900">
          {price} <span className="text-base font-normal text-slate-500">{t(`cycle.${tier.cycle}`)}</span>
        </p>
        {setupFee && (
          <p className="mt-2 text-sm text-slate-500">
            {t('subscribe.plusSetup', { fee: setupFee })}
          </p>
        )}
      </div>

      {ownedTenants.length > 0 && (
        <div className="mb-6">
          <label htmlFor="owner" className="mb-1 block text-sm font-medium text-slate-700">
            {t('subscribe.ownerLabel')}
          </label>
          <select
            id="owner"
            value={selectedOwner}
            onChange={(e) => setSelectedOwner(e.target.value)}
            className="block w-full rounded-md border border-slate-300 bg-white px-3 py-2 text-sm focus:border-slate-500 focus:outline-none focus:ring-1 focus:ring-slate-500"
          >
            <option value={PERSONAL_OPTION}>{t('subscribe.ownerPersonal')}</option>
            {ownedTenants.map((m) => (
              <option key={m.tenantUuid} value={m.tenantUuid}>
                {m.tenantUuid}
              </option>
            ))}
          </select>
          <p className="mt-1 text-xs text-slate-500">{t('subscribe.ownerHint')}</p>
        </div>
      )}

      <p className="mb-6 text-sm text-slate-600">{t('subscribe.cardNotice')}</p>

      {subscribeMutation.isError && (
        <p className="mb-4 rounded-md bg-red-50 px-3 py-2 text-sm text-red-700" role="alert">
          {subscribeMutation.error.message}
        </p>
      )}

      <button
        type="button"
        onClick={handleSubscribe}
        disabled={subscribeMutation.isPending || profileGate === 'checking'}
        className="inline-flex w-full items-center justify-center rounded-md bg-slate-900 px-4 py-2.5 text-sm font-medium text-white hover:bg-slate-700 disabled:cursor-not-allowed disabled:bg-slate-400"
      >
        {subscribeMutation.isPending || profileGate === 'checking'
          ? t('subscribe.submitting')
          : t('subscribe.submit')}
      </button>

      <button
        type="button"
        onClick={() => navigate(-1)}
        className="mt-3 inline-flex w-full items-center justify-center rounded-md px-4 py-2 text-sm font-medium text-slate-600 hover:text-slate-900"
      >
        {t('subscribe.cancel')}
      </button>
    </section>
  );
}
