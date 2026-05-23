import type { TFunction } from 'i18next';

const toAscii = (str: string) =>
  str
    .normalize('NFD')
    .replace(/[̀-ͯ]/g, '')
    .replace(/[^a-zA-Z0-9 ]+/g, ' ')
    .trim();

export const navItemKey = (name: string): string => {
  const words = toAscii(name).split(/\s+/).filter(Boolean);
  if (words.length === 0) return '';
  return words
    .map((w, i) =>
      i === 0
        ? w.toLowerCase()
        : w.charAt(0).toUpperCase() + w.slice(1).toLowerCase()
    )
    .join('');
};

export const translateNavRealm = (
  t: TFunction,
  realmKey: string,
  fallback: string
): string => t(`nav.realms.${realmKey}`, { defaultValue: fallback });

export const translateNavSection = (t: TFunction, label: string): string =>
  t(`nav.sections.${navItemKey(label)}`, { defaultValue: label });

export const translateNavItem = (t: TFunction, name: string): string =>
  t(`nav.items.${navItemKey(name)}`, { defaultValue: name });
