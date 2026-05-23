// Typed t() keys for react-i18next.
//
// Augments react-i18next's module so `t('app.name')` is checked against
// the actual shape of en.json at typecheck time. Misspelled or missing
// keys fail tsc instead of silently rendering as the literal string at
// runtime. Italian and English MUST share the same key tree — Phase 7
// adds a CI lint that enforces parity; until then, `t()` types come
// from en.json and `it.json` follows by convention.
//
// Imports en.json directly (tsconfig has `resolveJsonModule: true`),
// so adding a key to en.json widens the union automatically.
import 'react-i18next';
import en from './locales/en.json';

declare module 'react-i18next' {
  interface CustomTypeOptions {
    defaultNS: 'translation';
    resources: {
      translation: typeof en;
    };
  }
}
