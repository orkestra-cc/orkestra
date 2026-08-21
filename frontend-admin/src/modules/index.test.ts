import { describe, it, expect } from 'vitest';
import { moduleCatalog } from './index';

describe('moduleCatalog auto-discovery', () => {
  // The core-only base (ADR-0006) ships NO optional modules, so the glob over
  // `./*.tsx` discovers nothing. This also guards the invariant: an addon
  // manifest accidentally committed to the core base would make this fail.
  it('discovers no optional modules on the core-only base', () => {
    expect(Object.keys(moduleCatalog)).toEqual([]);
  });

  // Every discovered manifest (there are none here, but a fork's will) is keyed
  // by its own `name` and exposes the required manifest surface. Kept as
  // executable documentation of the discovery contract.
  it('keys each discovered manifest by name with a routes factory', () => {
    for (const [key, manifest] of Object.entries(moduleCatalog)) {
      expect(manifest.name).toBe(key);
      expect(typeof manifest.routes).toBe('function');
      if (manifest.injectApi)
        expect(typeof manifest.injectApi).toBe('function');
      if (manifest.globalOverlay)
        expect(typeof manifest.globalOverlay).toBe('object');
    }
  });
});
