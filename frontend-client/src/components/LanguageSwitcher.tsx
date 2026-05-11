import { useTranslation } from 'react-i18next';

const SUPPORTED = ['it', 'en'] as const;
type Lang = (typeof SUPPORTED)[number];

export function LanguageSwitcher() {
  const { i18n, t } = useTranslation();
  const current = (i18n.resolvedLanguage ?? 'it') as Lang;

  return (
    <label className="ml-2 flex items-center gap-2 text-sm text-slate-600">
      <span className="sr-only">{t('lang.switch')}</span>
      <select
        value={current}
        onChange={(e) => void i18n.changeLanguage(e.target.value)}
        className="rounded-md border border-slate-300 bg-white px-2 py-1 text-sm focus:border-slate-500 focus:outline-none"
      >
        {SUPPORTED.map((code) => (
          <option key={code} value={code}>
            {t(`lang.${code}`)}
          </option>
        ))}
      </select>
    </label>
  );
}
