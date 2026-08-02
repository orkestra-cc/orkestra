import type { TFunction } from 'i18next';
import i18n from '../i18n';
import type { ConfigField } from 'store/api/moduleApi';

/**
 * Resolves the display string for a module config field or group.
 *
 * The key is derived from the backend's already-stable `key`, so the schema
 * carries no redundant i18n field and `label` stays the literal that always
 * works. Resolution order, mirroring `helpers/navLabel.ts`:
 *
 *   1. `<moduleName>:config.fields.<key>.label` — a fork addon's own namespace (ADR-0007)
 *   2. `moduleConfig.<moduleName>.fields.<key>.label` — the core bundle
 *   3. the literal `label` the backend sent
 *
 * Step 3 is what keeps an un-migrated addon showing English instead of a raw
 * key path.
 */
const resolve = (
  t: TFunction,
  moduleName: string,
  suffix: string,
  fallback: string
): string => {
  if (moduleName && i18n.hasResourceBundle(i18n.language, moduleName)) {
    const scoped = t(`${moduleName}:config.${suffix}`, { defaultValue: '' });
    if (scoped) return scoped;
  }
  return t(`moduleConfig.${moduleName}.${suffix}`, { defaultValue: fallback });
};

export const translateConfigField = (
  t: TFunction,
  moduleName: string,
  field: ConfigField,
  part: 'label' | 'desc'
): string =>
  resolve(
    t,
    moduleName,
    `fields.${field.key}.${part}`,
    part === 'label' ? field.label : field.description || ''
  );

export const translateConfigGroup = (
  t: TFunction,
  moduleName: string,
  group: { key: string; label: string }
): string => resolve(t, moduleName, `groups.${group.key}.label`, group.label);
