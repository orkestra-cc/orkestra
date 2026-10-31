import { describe, it, expect, vi } from 'vitest';
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

describe('i18n language cookie', () => {
  it('detects and caches the language under the runtime cookie prefix', async () => {
    const runtime = window as unknown as { __ORKESTRA_CONFIG__?: unknown };
    runtime.__ORKESTRA_CONFIG__ = { cookiePrefix: 'stack2_' };
    try {
      vi.resetModules();
      const fresh = (await import('./i18n')).default;
      expect(fresh.options.detection?.lookupCookie).toBe('stack2_admin_lang');
    } finally {
      delete runtime.__ORKESTRA_CONFIG__;
      vi.resetModules();
    }
  });
});
