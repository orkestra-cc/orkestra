import { describe, it, expect } from 'vitest';
import { devProxy } from './devProxy';

describe('devProxy', () => {
  it('is off without a target', () => {
    expect(devProxy(undefined)).toBeUndefined();
    expect(devProxy('')).toBeUndefined();
  });

  it('forwards /v1 and /ws (websocket) to the target', () => {
    expect(devProxy('http://b:3000')).toEqual({
      '/v1': { target: 'http://b:3000' },
      '/ws': { target: 'http://b:3000', ws: true }
    });
  });

  it('does not rewrite the Host header (no changeOrigin)', () => {
    const proxy = devProxy('http://b:3000') as Record<string, object>;
    expect(proxy['/v1']).not.toHaveProperty('changeOrigin');
    expect(proxy['/ws']).not.toHaveProperty('changeOrigin');
  });

  describe('storage (object store behind the dev server)', () => {
    const storage = { target: 'http://rustfs:9000', prefix: 'owt-a' };

    it("forwards the slot's bucket paths to the object store, Host kept", () => {
      const proxy = devProxy('http://b:3000', storage) as Record<
        string,
        object
      >;
      expect(proxy['^/owt-a-[a-z0-9-]+/']).toEqual({
        target: 'http://rustfs:9000'
      });
      // The presigned signature covers the Host header: it must reach the
      // store as the browser sent it.
      expect(proxy['^/owt-a-[a-z0-9-]+/']).not.toHaveProperty('changeOrigin');
      expect(proxy['/v1']).toEqual({ target: 'http://b:3000' });
    });

    it('adds nothing without a target or with an unsafe prefix', () => {
      for (const s of [
        { target: '', prefix: 'owt-a' },
        { target: 'http://rustfs:9000', prefix: '' },
        { target: 'http://rustfs:9000', prefix: 'owt a' },
        { target: 'http://rustfs:9000', prefix: '.*' },
        { target: 'http://rustfs:9000', prefix: 'Owt-A' },
        undefined
      ]) {
        expect(Object.keys(devProxy('http://b:3000', s) ?? {})).toEqual([
          '/v1',
          '/ws'
        ]);
      }
    });

    it('is still off without an API target', () => {
      expect(devProxy(undefined, storage)).toBeUndefined();
    });
  });
});
