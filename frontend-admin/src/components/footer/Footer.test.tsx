import { describe, it, expect, beforeEach, vi } from 'vitest';
import { renderWithProviders } from 'test/render';

// Footer reads the frozen `config` singleton; mock it so we control the
// values. A single stable object is mutated per-test (Footer reads
// config.* at render time, so mutation is observed).
const { mockConfig } = vi.hoisted(() => ({
  mockConfig: {
    env: '',
    version: '',
    appName: '',
    cloneVersion: '',
    buildCommit: '',
    startedAt: ''
  }
}));
// `default` is included alongside the named `config` export because
// renderWithProviders pulls in the full Redux store, whose baseApi.ts does
// `import runtimeConfig from 'config/environment'` (default import) to read
// `apiUrl` when constructing the RTK Query base query. Mocking only the
// named export leaves the default undefined and crashes store setup before
// Footer ever renders.
vi.mock('config/environment', () => ({
  config: mockConfig,
  default: mockConfig
}));

import Footer from './Footer';

describe('Footer', () => {
  beforeEach(() => {
    Object.assign(mockConfig, {
      env: 'development',
      version: '0.3.15',
      appName: 'orkestra-commons',
      cloneVersion: 'v1.2.0',
      buildCommit: 'a1b2c3d',
      startedAt: '2026-07-13 10:22Z'
    });
  });

  it('renders the full deployment fingerprint', () => {
    const { container } = renderWithProviders(<Footer />);
    const text = container.textContent ?? '';
    expect(text).toContain('Orkestra v0.3.15');
    expect(text).toContain('orkestra-commons');
    expect(text).toContain('development');
    expect(text).toContain('clone v1.2.0');
    expect(text).toContain('build a1b2c3d');
    expect(text).toContain('2026-07-13 10:22Z');
  });

  it('omits clone/build/startedAt on a fresh checkout', () => {
    Object.assign(mockConfig, {
      cloneVersion: 'dev',
      buildCommit: '',
      startedAt: ''
    });
    const { container } = renderWithProviders(<Footer />);
    const text = container.textContent ?? '';
    expect(text).toContain('Orkestra v0.3.15');
    expect(text).toContain('orkestra-commons');
    expect(text).not.toContain('clone');
    expect(text).not.toContain('build ');
  });
});
