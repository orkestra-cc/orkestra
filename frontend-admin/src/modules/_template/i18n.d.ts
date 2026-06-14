// Type augmentation for THIS addon's i18next namespace (ADR-0007).
//
// Copy alongside your module's locale bundles (e.g. src/pages/<name>/i18n.d.ts)
// and replace `widgets` with your module name — it MUST match the manifest
// `name` and the namespace you register via `injectI18n`. Module augmentation
// merges, so `t('widgets:list.title')` becomes typed WITHOUT touching the core
// `src/i18n-types.d.ts`. en.json is the source of truth; it.json follows it
// (the parity test enforces the match).
//
// NB: `_template` is excluded from tsconfig, so this file is inert here — it
// only takes effect once copied into an included location.
import 'react-i18next';
import type widgetsEn from './locales/en.json';

declare module 'react-i18next' {
  interface CustomTypeOptions {
    resources: {
      widgets: typeof widgetsEn;
    };
  }
}
