import { describe, it, expect } from 'vitest';
import i18n from './i18n';

// Smoke tests for the i18n bootstrap. The seed under locales/{en,it}.json
// is intentionally minimal (just app.name) — these assertions prove the
// wiring is live and supportedLngs accepts every locale we ship before
// Phase 4 adds real strings.

describe('i18n bootstrap', () => {
  it('initializes with English as the default language', () => {
    expect(i18n.language).toBe('en');
    expect(i18n.options.fallbackLng).toEqual(['en']);
  });

  it('resolves app.name in English', () => {
    expect(i18n.t('app.name')).toBe('Orkestra');
  });

  it('resolves app.name in Italian after changeLanguage', async () => {
    await i18n.changeLanguage('it');
    try {
      expect(i18n.t('app.name')).toBe('Orkestra');
      expect(i18n.language).toBe('it');
    } finally {
      // Reset so subsequent tests start from EN — the i18n singleton
      // outlives any single test's lifecycle.
      await i18n.changeLanguage('en');
    }
  });

  it('lists en + it as the supported locales', () => {
    expect(i18n.options.supportedLngs).toContain('en');
    expect(i18n.options.supportedLngs).toContain('it');
  });
});
