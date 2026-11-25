import type { ComponentType, LazyExoticComponent } from 'react';
import type { RouteObject } from 'react-router';

/**
 * A UI piece a module contributes to a slot owned by another module or by the
 * core. The slot owner defines the props `component` receives and decides where
 * contributions render; the core only collects them (useSlotContributions),
 * for active modules and the caller's permissions.
 */
export interface SlotContribution {
  /**
   * Stable id, unique app-wide: MUST start with `<module name>.`
   * (e.g. 'widgets.summary'). A contribution with any other key is dropped.
   */
  key: string;
  /** i18n key in the contributing module's own namespace (ADR-0007). */
  labelKey: string;
  /** Optional permission key; the contribution is hidden without it. */
  permission?: string;
  /** Lazily loaded component; its props are defined by the slot owner (hence `any`). */
  component: LazyExoticComponent<ComponentType<any>>;
}

/** Props every addon section of the module detail page receives. */
export interface ModuleAdminSectionProps {
  moduleName: string;
  /** The environment the module page is showing (?env=). */
  environment: string;
  /** True while the page's shared config form has unsaved edits. */
  configDirty: boolean;
  /**
   * Report a write in flight. A boolean state, not a counter: pass true while
   * a write is in flight and false when it settles, and again (false) on
   * unmount. While true the host refuses to leave the section until it
   * settles: the page's rail and environment switcher are locked and any
   * route or query change asks for confirmation first.
   */
  onBusyChange: (busy: boolean) => void;
}

/** A section an addon contributes to /admin/modules/:name. */
export interface ModuleAdminSection {
  /**
   * Reached at ?section=__addon:<key>. Must match `^[a-z0-9][a-z0-9_-]*$` and
   * be unique within the manifest: the first duplicate wins, and invalid
   * entries are not mounted.
   */
  key: string;
  /**
   * A key inside the module's own i18n namespace (ADR-0007), resolved as
   * `t('<moduleName>:<labelKey>')` for the section's title.
   */
  labelKey: string;
  /**
   * The host wraps it in Suspense and mounts it only while its section is
   * shown (the selected rail entry, or always on the stacked page).
   */
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
  /**
   * Contributions to slots owned by other modules, keyed by slot id (a string
   * the slot owner publishes). Rendered only while this module is active and
   * the caller holds each contribution's `permission`.
   */
  contributions?: Record<string, SlotContribution[]>;
}
