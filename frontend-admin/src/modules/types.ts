import type { ComponentType, LazyExoticComponent } from 'react';
import type { RouteObject } from 'react-router';

/** Props every addon section of the module detail page receives. */
export interface ModuleAdminSectionProps {
  moduleName: string;
  /** The environment the module page is showing (?env=). */
  environment: string;
  /** True while the page's shared config form has unsaved edits. */
  configDirty: boolean;
  /**
   * Report a write in flight. While busy the page disables its rail and
   * environment switcher, and its single navigation blocker holds any route
   * change. Call with false when the write settles and on unmount.
   */
  onBusyChange: (busy: boolean) => void;
}

/** A section an addon contributes to /admin/modules/:name. */
export interface ModuleAdminSection {
  /**
   * Reached at ?section=__addon:<key>. Must match ADMIN_SECTION_KEY and be
   * unique within the manifest; invalid entries are not mounted.
   */
  key: string;
  /** i18n key in the module's own namespace (ADR-0007). */
  labelKey: string;
  component: LazyExoticComponent<ComponentType<ModuleAdminSectionProps>>;
}

export interface ModuleManifest {
  /** Must match backend module name exactly (e.g. 'billing', 'sales') */
  name: string;
  /** Returns route objects for this module (components use React.lazy inside) */
  routes: () => RouteObject[];
  /** Optional app-wide surface, mounted only while this module is visible. */
  globalOverlay?: LazyExoticComponent<ComponentType>;
  /** Optional top-navbar action, mounted only while this module is visible. */
  globalNavAction?: LazyExoticComponent<ComponentType>;
  /** Optional sections of the module's admin page (/admin/modules/:name). */
  adminSections?: ModuleAdminSection[];
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
