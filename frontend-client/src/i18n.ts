import i18n from 'i18next';
import LanguageDetector from 'i18next-browser-languagedetector';
import { initReactI18next } from 'react-i18next';

import en from '@/locales/en.json';
import it from '@/locales/it.json';

// react-i18next bootstrap. Italian is the default; English is the fallback
// so a missing key never renders as a raw lookup string. Detection order is
// querystring → cookie → browser, with a 30-day cookie so a chosen
// language survives reloads. Add a new locale by dropping a JSON file
// under src/locales and registering it in `resources` below — no other
// scaffolding changes required.
void i18n
  .use(LanguageDetector)
  .use(initReactI18next)
  .init({
    resources: {
      en: { translation: en },
      it: { translation: it },
    },
    fallbackLng: 'en',
    supportedLngs: ['it', 'en'],
    interpolation: { escapeValue: false },
    detection: {
      order: ['querystring', 'cookie', 'navigator', 'htmlTag'],
      caches: ['cookie'],
      lookupQuerystring: 'lang',
      lookupCookie: 'orkestra_client_lang',
      cookieMinutes: 60 * 24 * 30,
    },
  });

export default i18n;
