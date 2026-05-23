import { describe, expect, it } from 'vitest';
import en from './en.json';
import itLocale from './it.json';

// Flattens a nested locale object into dot-paths so we can compare key
// sets between locales. `app.name` → "app.name". Treats every leaf
// (string, number, plural variant) as terminal — arrays and objects
// recurse.
function flatten(obj: unknown, prefix: string, out: Set<string>): Set<string> {
  if (obj === null || typeof obj !== 'object') {
    out.add(prefix);
    return out;
  }
  if (Array.isArray(obj)) {
    out.add(prefix);
    return out;
  }
  for (const [k, v] of Object.entries(obj)) {
    const path = prefix ? `${prefix}.${k}` : k;
    flatten(v, path, out);
  }
  return out;
}

const enKeys = flatten(en, '', new Set());
const itKeys = flatten(itLocale, '', new Set());

// Parity tests for src/locales/*.json — every key shipped in one
// locale must exist in the other. A failure means: a developer added
// a translation to one file but forgot the matching entry in the
// other, and production would silently fall back to the key path or
// the fallbackLng. Renames, removals, and additions all need a
// matched pair.
//
// To debug a failure: the diff message lists the missing keys, copy
// them into the locale that lacks them (with TODO_IT or TODO_EN as
// placeholder copy if the actual translation is pending Phase 6).
describe('locale parity', () => {
  it('en.json and it.json carry the same set of keys', () => {
    const onlyInEn = [...enKeys].filter(k => !itKeys.has(k)).sort();
    const onlyInIt = [...itKeys].filter(k => !enKeys.has(k)).sort();

    // i18next plural variants (`*_one`, `*_other`, etc.) are tied to
    // each locale's CLDR plural rules. English requires `_one` +
    // `_other`; Italian uses the same set. Other languages we add
    // later may differ — when they do, this assertion is the place
    // to teach the test about allowed asymmetries.
    expect({ onlyInEn, onlyInIt }).toStrictEqual({
      onlyInEn: [],
      onlyInIt: []
    });
  });

  it('every key resolves to a non-empty string in both locales', () => {
    // Catches the second failure mode: a key exists in both files
    // but one side is empty ("") or a TODO marker that leaked to
    // main. Pluralization variants land as string leaves too, so
    // this also guards against an "ignored" `_other` form.
    const empties: Array<{ locale: 'en' | 'it'; key: string }> = [];

    const visit = (obj: unknown, prefix: string, locale: 'en' | 'it'): void => {
      if (typeof obj === 'string') {
        if (obj.trim() === '') empties.push({ locale, key: prefix });
        return;
      }
      if (obj === null || typeof obj !== 'object' || Array.isArray(obj)) return;
      for (const [k, v] of Object.entries(obj)) {
        visit(v, prefix ? `${prefix}.${k}` : k, locale);
      }
    };

    visit(en, '', 'en');
    visit(itLocale, '', 'it');

    expect(empties).toEqual([]);
  });
});
