import { Link, useParams } from 'react-router-dom';
import { useQuery } from '@tanstack/react-query';
import { useTranslation } from 'react-i18next';

import { listPublicCatalog, findServiceByCode } from '@/api/catalog';
import { formatPrice } from '@/lib/format';
import { useAuth } from '@/auth/useAuth';

export function CatalogServicePage() {
  const { code = '' } = useParams<{ code: string }>();
  const { t, i18n } = useTranslation();
  const { isAuthenticated } = useAuth();

  const { data, isLoading, isError } = useQuery({
    queryKey: ['catalog'],
    queryFn: ({ signal }) => listPublicCatalog(signal),
    staleTime: 60_000,
  });

  const service = findServiceByCode(data?.items, code);
  const language = i18n.resolvedLanguage ?? 'it';

  return (
    <section className="mx-auto max-w-4xl px-6 py-16">
      <Link to="/catalog" className="mb-8 inline-block text-sm text-slate-600 hover:underline">
        ← {t('catalog.back')}
      </Link>

      {isLoading && <p className="text-slate-500">{t('loading')}</p>}
      {isError && (
        <p className="text-red-600" role="alert">
          {t('error.generic')}
        </p>
      )}

      {data && !service && (
        <p className="text-slate-600" role="alert">
          {t('catalog.empty')}
        </p>
      )}

      {service && (
        <article>
          {service.category && (
            <p className="mb-2 text-xs font-medium uppercase tracking-wider text-slate-500">
              {t('catalog.category')} · {service.category}
            </p>
          )}
          <h1 className="mb-4 text-4xl font-semibold tracking-tight">{service.name}</h1>
          {service.description && (
            <p className="mb-10 text-lg text-slate-600">{service.description}</p>
          )}

          {service.setupFeeCents && service.setupFeeCents > 0 ? (
            <p className="mb-6 text-sm text-slate-500">
              {t('catalog.setupFee')}:{' '}
              <span className="font-semibold text-slate-900">
                {formatPrice(service.setupFeeCents, service.pricingTiers[0]?.currency ?? 'EUR', language)}
              </span>
            </p>
          ) : null}

          <ul className="grid grid-cols-1 gap-4 md:grid-cols-2">
            {service.pricingTiers.map((tier) => (
              <li
                key={tier.code}
                className="rounded-lg border border-slate-200 bg-white p-6 shadow-sm"
              >
                <p className="mb-2 text-sm font-medium uppercase tracking-wider text-slate-500">
                  {tier.code}
                </p>
                <p className="mb-4">
                  <span className="text-3xl font-semibold tracking-tight text-slate-900">
                    {formatPrice(tier.amountCents, tier.currency, language)}
                  </span>{' '}
                  <span className="text-slate-500">{t(`cycle.${tier.cycle}`)}</span>
                </p>
                {tier.capabilities && tier.capabilities.length > 0 && (
                  <div className="mb-6">
                    <p className="mb-2 text-xs font-medium uppercase tracking-wider text-slate-500">
                      {t('catalog.capabilities')}
                    </p>
                    <ul className="space-y-1 text-sm text-slate-700">
                      {tier.capabilities.map((cap) => (
                        <li key={cap}>· {cap}</li>
                      ))}
                    </ul>
                  </div>
                )}
                <Link
                  to={`${isAuthenticated ? '/subscribe' : '/signup'}?service=${encodeURIComponent(service.code)}&tier=${encodeURIComponent(tier.code)}`}
                  className="inline-flex w-full items-center justify-center rounded-md bg-slate-900 px-4 py-2 text-sm font-medium text-white hover:bg-slate-700"
                >
                  {t('catalog.subscribe')}
                </Link>
              </li>
            ))}
          </ul>
        </article>
      )}
    </section>
  );
}
