import { describe, it, expect, beforeEach } from 'vitest';
import {
  OAUTH_RETURN_TO_KEY,
  OAUTH_RETURN_TO_TTL_MS,
  stashOAuthReturnTo,
  takeOAuthReturnTo
} from './socialAuthUtils';

describe('OAuth return-target stash', () => {
  beforeEach(() => sessionStorage.clear());

  it('round-trips a safe target within ten minutes and deletes it on take', () => {
    stashOAuthReturnTo('/admin/modules?tab=x', 1_000);
    expect(takeOAuthReturnTo(1_000 + OAUTH_RETURN_TO_TTL_MS)).toBe(
      '/admin/modules?tab=x'
    );
    expect(sessionStorage.getItem(OAUTH_RETURN_TO_KEY)).toBeNull();
    expect(takeOAuthReturnTo(2_000)).toBeNull();
  });

  it('ignores a stale record but still deletes it', () => {
    stashOAuthReturnTo('/admin/modules', 1_000);
    expect(takeOAuthReturnTo(1_000 + OAUTH_RETURN_TO_TTL_MS + 1)).toBeNull();
    expect(sessionStorage.getItem(OAUTH_RETURN_TO_KEY)).toBeNull();
  });

  it('ignores a record from the future', () => {
    stashOAuthReturnTo('/admin/modules', 5_000);
    expect(takeOAuthReturnTo(4_999)).toBeNull();
  });

  it('never stashes an unsafe target and clears a stale one instead', () => {
    sessionStorage.setItem(
      OAUTH_RETURN_TO_KEY,
      JSON.stringify({ target: '/old', createdAt: 1 })
    );
    stashOAuthReturnTo('//evil.example', 1_000);
    expect(sessionStorage.getItem(OAUTH_RETURN_TO_KEY)).toBeNull();
    stashOAuthReturnTo(null, 1_000);
    expect(sessionStorage.getItem(OAUTH_RETURN_TO_KEY)).toBeNull();
  });

  it('re-sanitises on take (sessionStorage is client-writable)', () => {
    sessionStorage.setItem(
      OAUTH_RETURN_TO_KEY,
      JSON.stringify({ target: 'https://evil.example', createdAt: 1_000 })
    );
    expect(takeOAuthReturnTo(1_001)).toBeNull();
    sessionStorage.setItem(
      OAUTH_RETURN_TO_KEY,
      JSON.stringify({ target: '/login', createdAt: 1_000 })
    );
    expect(takeOAuthReturnTo(1_001)).toBeNull();
    sessionStorage.setItem(OAUTH_RETURN_TO_KEY, '/legacy-plain-string');
    expect(takeOAuthReturnTo(1_001)).toBeNull();
    sessionStorage.setItem(
      OAUTH_RETURN_TO_KEY,
      JSON.stringify({ target: '/ok', createdAt: 'yesterday' })
    );
    expect(takeOAuthReturnTo(1_001)).toBeNull();
  });
});
