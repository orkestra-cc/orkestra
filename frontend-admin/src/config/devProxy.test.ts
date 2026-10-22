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
});
