import { describe, expect, it, vi } from 'vitest';

import {
  STALE_CHUNK_RELOAD_KEY,
  isStaleChunkError,
  reloadForStaleChunk,
  staleChunkReloadAllowed
} from './staleChunk';

const memoryStorage = (initial: Record<string, string> = {}) => {
  const data = { ...initial };
  return {
    getItem: (k: string) => (k in data ? data[k] : null),
    setItem: (k: string, v: string) => {
      data[k] = v;
    },
    data
  };
};

describe('isStaleChunkError', () => {
  it.each([
    'Failed to fetch dynamically imported module: https://x/assets/UserDashboard-a1b2.js',
    'error loading dynamically imported module: https://x/assets/a.js',
    'Importing a module script failed.',
    'Unable to preload CSS for /assets/index-a1b2.css'
  ])('recognizes %s', message => {
    expect(isStaleChunkError(new TypeError(message))).toBe(true);
  });

  it('ignores ordinary errors and non-errors', () => {
    expect(
      isStaleChunkError(new Error('Cannot read properties of undefined'))
    ).toBe(false);
    expect(isStaleChunkError({ status: 404 })).toBe(false);
    expect(isStaleChunkError(undefined)).toBe(false);
  });
});

describe('reloadForStaleChunk', () => {
  it('reloads once and records when', () => {
    const storage = memoryStorage();
    const reload = vi.fn();

    expect(reloadForStaleChunk({ now: 1_000, storage, reload })).toBe(true);
    expect(reload).toHaveBeenCalledTimes(1);
    expect(storage.data[STALE_CHUNK_RELOAD_KEY]).toBe('1000');
  });

  it('does not reload again right after a reload, so a broken build cannot loop', () => {
    const storage = memoryStorage({ [STALE_CHUNK_RELOAD_KEY]: '1000' });
    const reload = vi.fn();

    expect(staleChunkReloadAllowed({ now: 5_000, storage })).toBe(false);
    expect(reloadForStaleChunk({ now: 5_000, storage, reload })).toBe(false);
    expect(reload).not.toHaveBeenCalled();
  });

  it('reloads again for a later deploy', () => {
    const storage = memoryStorage({ [STALE_CHUNK_RELOAD_KEY]: '1000' });
    const reload = vi.fn();

    expect(reloadForStaleChunk({ now: 61_000, storage, reload })).toBe(true);
    expect(reload).toHaveBeenCalledTimes(1);
  });

  it('does not reload when the session storage is unavailable', () => {
    const storage = {
      getItem: () => {
        throw new Error('blocked');
      },
      setItem: () => {
        throw new Error('blocked');
      }
    };
    const reload = vi.fn();

    expect(reloadForStaleChunk({ now: 1_000, storage, reload })).toBe(false);
    expect(reload).not.toHaveBeenCalled();
  });
});
