import type { RouteObject } from 'react-router';

export interface ModuleManifest {
  /** Must match backend module name exactly (e.g. 'billing', 'sales') */
  name: string;
  /** Returns route objects for this module (components use React.lazy inside) */
  routes: () => RouteObject[];
  /** Dynamically imports the API slice file, triggering injectEndpoints as a side effect */
  injectApi?: () => Promise<unknown>;
  /**
   * Lazily imports the module's locale bundles, registered at boot as the
   * i18next namespace `name` (ADR-0007). Returns one entry per supported
   * language: `{ en: {...}, it: {...} }`. Consume the strings with
   * `useTranslation('<name>')` or the `<name>:` prefix — never write to the
   * core `src/locales/*.json`. Mirrors `injectApi`: the bundles ship as
   * separate dynamically-imported chunks.
   */
  injectI18n?: () => Promise<Record<string, Record<string, unknown>>>;
}
