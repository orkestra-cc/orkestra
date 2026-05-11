import { Link } from 'react-router-dom';
import { useTranslation } from 'react-i18next';

export function HomePage() {
  const { t } = useTranslation();
  return (
    <section className="mx-auto max-w-4xl px-6 py-24 text-center">
      <p className="mb-3 text-sm font-medium uppercase tracking-wider text-slate-500">
        {t('app.tagline')}
      </p>
      <h1 className="mb-6 text-4xl font-semibold tracking-tight text-slate-900 sm:text-5xl">
        {t('home.hero')}
      </h1>
      <p className="mx-auto mb-10 max-w-2xl text-lg text-slate-600">
        {t('home.subhero')}
      </p>
      <Link
        to="/catalog"
        className="inline-flex items-center rounded-md bg-slate-900 px-5 py-3 text-base font-medium text-white shadow-sm hover:bg-slate-700"
      >
        {t('home.cta')}
      </Link>
    </section>
  );
}
