import { Link } from 'react-router-dom';
import { useQuery } from '@tanstack/react-query';
import { useTranslation } from 'react-i18next';

import { listPublicCatalog, type PublicCatalogService } from '@/api/catalog';
import { formatPrice } from '@/lib/format';

export function CatalogPage() {
  const { t, i18n } = useTranslation();
  const { data, isLoading, isError } = useQuery({
    queryKey: ['catalog'],
    queryFn: ({ signal }) => listPublicCatalog(signal),
    staleTime: 60_000,
  });

  return (
    <section className="mx-auto max-w-6xl px-6 py-16">
      <header className="mb-10">
        <h1 className="mb-2 text-3xl font-semibold tracking-tight">{t('catalog.title')}</h1>
        <p className="text-slate-600">{t('catalog.subtitle')}</p>
      </header>

      {isLoading && (
        <p className="text-slate-500" role="status">
          {t('loading')}
        </p>
      )}
      {isError && (
        <p className="text-red-600" role="alert">
          {t('error.generic')}
        </p>
      )}
      {data && data.items.length === 0 && (
        <p className="text-slate-500">{t('catalog.empty')}</p>
      )}

      <ul className="grid grid-cols-1 gap-6 sm:grid-cols-2 lg:grid-cols-3">
        {data?.items.map((service) => (
          <li key={service.code}>
            <ServiceCard service={service} language={i18n.resolvedLanguage ?? 'it'} />
          </li>
        ))}
      </ul>
    </section>
  );
}

interface ServiceCardProps {
  service: PublicCatalogService;
  language: string;
}

function ServiceCard({ service, language }: ServiceCardProps) {
  const { t } = useTranslation();
  const cheapest = service.pricingTiers.reduce<typeof service.pricingTiers[number] | undefined>(
    (acc, tier) => (acc === undefined || tier.amountCents < acc.amountCents ? tier : acc),
    undefined,
  );

  return (
    <Link
      to={`/catalog/${encodeURIComponent(service.code)}`}
      className="group block h-full rounded-lg border border-slate-200 bg-white p-6 shadow-sm transition-shadow hover:border-slate-300 hover:shadow-md"
    >
      {service.category && (
        <p className="mb-2 text-xs font-medium uppercase tracking-wider text-slate-500">
          {service.category}
        </p>
      )}
      <h2 className="mb-2 text-lg font-semibold text-slate-900 group-hover:text-slate-700">
        {service.name}
      </h2>
      {service.description && (
        <p className="mb-4 line-clamp-3 text-sm text-slate-600">{service.description}</p>
      )}
      <div className="mt-auto pt-4">
        {cheapest ? (
          <p className="text-sm text-slate-500">
            {t('catalog.from')}{' '}
            <span className="text-base font-semibold text-slate-900">
              {formatPrice(cheapest.amountCents, cheapest.currency, language)}
            </span>{' '}
            <span className="text-slate-500">{t(`cycle.${cheapest.cycle}`)}</span>
          </p>
        ) : (
          <p className="text-sm text-slate-500">{t('catalog.noPricing')}</p>
        )}
      </div>
    </Link>
  );
}
