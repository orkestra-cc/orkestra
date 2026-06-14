import { describe, expect, it } from 'vitest';
import en from './en.json';
import itLocale from './it.json';
import { emptyValues, keyDiff } from './parityCheck';

// Parity tests for the CORE src/locales/*.json — every key shipped in one
// locale must exist in the other, and every value must be a non-empty
// string. A failure means a developer added a translation to one file but
// forgot the matching entry in the other (production would silently fall
// back to the key path or the fallbackLng), or left an empty/TODO value.
//
// The comparison primitives live in `./parityCheck.ts` so an addon can reuse
// them on its OWN namespace bundles without touching these core files
// (ADR-0007). To debug a failure: the diff message lists the offending keys.
describe('locale parity', () => {
  it('en.json and it.json carry the same set of keys', () => {
    // i18next plural variants (`*_one`, `*_other`) follow each locale's CLDR
    // rules. English and Italian share the same set today; a future locale
    // that differs would teach this assertion about allowed asymmetries.
    const { onlyInA: onlyInEn, onlyInB: onlyInIt } = keyDiff(en, itLocale);
    expect({ onlyInEn, onlyInIt }).toStrictEqual({
      onlyInEn: [],
      onlyInIt: []
    });
  });

  it('every key resolves to a non-empty string in both locales', () => {
    const empties = [...emptyValues(en, 'en'), ...emptyValues(itLocale, 'it')];
    expect(empties).toEqual([]);
  });
});
