// i18n bootstrap for the operator console.
//
// English is the default. Detector order: cookie → navigator → fallback.
// Phase 3 ships the wiring with a minimal locales/{en,it}.json seed;
// Phase 4 extracts strings module-by-module. Adding a new locale = new
// JSON file + entry in `resources` + entry in `supportedLngs` + the
// `validate:"oneof=..."` allowlist on the backend's UpdateUserInput.
//
// See ../docs/plans/frontend-admin-i18n.md and frontend-client/src/i18n.ts
// for the sibling SPA's setup.
import i18n from 'i18next';
import LanguageDetector from 'i18next-browser-languagedetector';
import { initReactI18next } from 'react-i18next';

import en from './locales/en.json';
import it from './locales/it.json';

void i18n
  .use(LanguageDetector)
  .use(initReactI18next)
  .init({
    resources: {
      en: { translation: en },
      it: { translation: it }
    },
    lng: 'en',
    fallbackLng: 'en',
    supportedLngs: ['en', 'it'],
    interpolation: { escapeValue: false },
    detection: {
      // No querystring — the operator console is not deep-linked from
      // shareable URLs the way frontend-client is. Cookie is the
      // post-login persistence; navigator is the pre-login guess.
      order: ['cookie', 'navigator'],
      caches: ['cookie'],
      // Distinct from the client SPA's `orkestra_client_lang` so the
      // two surfaces can diverge per browser without bleeding state.
      lookupCookie: 'orkestra_admin_lang',
      cookieMinutes: 60 * 24 * 30
    }
  });

export default i18n;
