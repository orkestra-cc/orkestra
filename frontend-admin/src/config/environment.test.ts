import { describe, it, expect, beforeEach, vi } from 'vitest';

// The `config` singleton is frozen at module-import time from
// window.__ORKESTRA_CONFIG__, so each test seeds the global then imports
// a fresh module copy via vi.resetModules() + dynamic import.
describe('runtime config — footer fields', () => {
  beforeEach(() => {
    delete (window as unknown as { __ORKESTRA_CONFIG__?: unknown })
      .__ORKESTRA_CONFIG__;
  });

  it('reads the footer fields from window.__ORKESTRA_CONFIG__', async () => {
    (
      window as unknown as { __ORKESTRA_CONFIG__?: unknown }
    ).__ORKESTRA_CONFIG__ = {
      env: 'production',
      version: '0.3.15',
      appName: 'orkestra-commons',
      cloneVersion: 'v1.2.0',
      buildCommit: 'a1b2c3d',
      startedAt: '2026-07-13 10:22Z'
    };
    vi.resetModules();
    const { config } = await import('./environment');
    expect(config.version).toBe('0.3.15');
    expect(config.appName).toBe('orkestra-commons');
    expect(config.cloneVersion).toBe('v1.2.0');
    expect(config.buildCommit).toBe('a1b2c3d');
    expect(config.startedAt).toBe('2026-07-13 10:22Z');
  });

  it('defaults gracefully when the fields are absent', async () => {
    (
      window as unknown as { __ORKESTRA_CONFIG__?: unknown }
    ).__ORKESTRA_CONFIG__ = { env: 'development' };
    vi.resetModules();
    const { config } = await import('./environment');
    expect(config.appName).toBe('');
    expect(config.cloneVersion).toBe('dev');
    expect(config.buildCommit).toBe('');
    expect(config.startedAt).toBe('');
  });
});
