import { useEffect, useRef } from 'react';
import { moduleCatalog } from 'modules';
import i18n from '../i18n';

/**
 * Registers every catalogued addon's locale bundles as a dedicated i18next
 * namespace named after the module (ADR-0007). Call once at app root, next
 * to `useModuleApiInjection`.
 *
 * Deliberately NOT gated on auth or enabled-state (unlike `injectApi`, which
 * waits on the admin-only modules query): an addon page can render for a
 * non-admin operator, so its namespace must exist regardless of who is
 * signed in. Loading the locale chunk of a disabled module is harmless — its
 * routes are gated by `ModuleGate`, so its pages never render — and the JSON
 * is small. Bundles ship per-language at once, so a later
 * `i18n.changeLanguage` finds the namespace already registered.
 */
export function useModuleI18nInjection(): void {
  const injectedRef = useRef<Set<string>>(new Set());

  useEffect(() => {
    for (const [name, manifest] of Object.entries(moduleCatalog)) {
      if (!manifest.injectI18n) continue;
      if (injectedRef.current.has(name)) continue;
      injectedRef.current.add(name);

      void manifest.injectI18n().then(bundles => {
        for (const [lng, resources] of Object.entries(bundles)) {
          // deep=true merges nested keys; overwrite=false guarantees an
          // addon can never clobber a core key or another addon's namespace.
          i18n.addResourceBundle(lng, name, resources, true, false);
        }
      });
    }
  }, []);
}

export default useModuleI18nInjection;
