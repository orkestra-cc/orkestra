import i18n from '../i18n';

/** The shape every backend error carries: a machine code + English detail. */
export interface ApiErrorBody {
  code?: string;
  detail?: string;
}

/**
 * Resolves a backend error `{ code, detail }` to a localized message,
 * preferring an addon's own i18next namespace over the core `errors.*`
 * keys (ADR-0007). Resolution order:
 *
 *  1. **Addon namespace** — for a dotted code `<module>.<rest>` (e.g.
 *     `billing.invoice_overdue`), try `<module>:errors.<rest>`. This lets an
 *     addon localize its own error codes inside its own namespace bundle,
 *     without ever adding keys to the core `src/locales/*.json`.
 *  2. **Core namespace** — `errors.<code>`, the historical nested lookup
 *     (e.g. `errors.auth.email_in_use`). Core codes resolve here unchanged.
 *  3. **Backend detail** — the English fallback the handler returned.
 *  4. **Explicit fallback / raw code** — last resort so the UI is never blank.
 *
 * Opt-in: existing core call sites keep their inline `t('errors.<code>')`
 * logic; new addon code (and any core site that wants the namespaced path)
 * calls this instead.
 */
export function resolveErrorMessage(
  err: ApiErrorBody | undefined | null,
  fallback?: string
): string {
  const code = err?.code;
  if (code) {
    const dot = code.indexOf('.');
    if (dot > 0) {
      const ns = code.slice(0, dot);
      const rest = code.slice(dot + 1);
      const nsKey = `${ns}:errors.${rest}`;
      if (i18n.exists(nsKey)) return i18n.t(nsKey);
    }
    const coreKey = `errors.${code}`;
    if (i18n.exists(coreKey)) return i18n.t(coreKey);
  }
  return err?.detail || fallback || code || '';
}

export default resolveErrorMessage;
