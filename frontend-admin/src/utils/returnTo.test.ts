import { describe, it, expect } from 'vitest';
import {
  DEFAULT_POST_LOGIN,
  locationToReturnTo,
  sanitizeReturnTo
} from './returnTo';

describe('sanitizeReturnTo', () => {
  it('accepts a plain in-app path', () => {
    expect(sanitizeReturnTo('/admin/modules')).toBe('/admin/modules');
  });

  it('preserves search and hash', () => {
    expect(sanitizeReturnTo('/admin/modules?tab=addons#row-3')).toBe(
      '/admin/modules?tab=addons#row-3'
    );
  });

  it.each([
    ['null', null],
    ['undefined', undefined],
    ['non-string', 42],
    ['empty string', ''],
    ['whitespace only', '   ']
  ])('returns null for %s', (_label, input) => {
    expect(sanitizeReturnTo(input as unknown)).toBeNull();
  });

  it.each([
    // open redirect — absolute URLs
    'https://evil.com',
    'http://evil.com/admin',
    // protocol-relative
    '//evil.com',
    '//evil.com/admin/modules',
    // backslash variants browsers normalise to "//"
    '/\\evil.com',
    '/\\\\evil.com',
    // bare path without leading slash
    'admin/modules',
    // encoded slash/backslash leaders
    '/%2f%2fevil.com',
    '/%5cevil.com',
    // a path containing a backslash anywhere
    '/admin\\..\\evil',
    // control characters
    '/admin\n/modules',
    '/admin\tmodules'
  ])('rejects open-redirect vector %s', vector => {
    expect(sanitizeReturnTo(vector)).toBeNull();
  });

  it.each([
    '/login',
    '/login?next=/admin',
    '/mfa/verify',
    '/auth/callback',
    '/register',
    '/forgot-password',
    '/reset-password',
    '/verify-email',
    '/setup',
    '/landing',
    '/logout'
  ])('rejects auth-flow loop target %s', authPath => {
    expect(sanitizeReturnTo(authPath)).toBeNull();
  });

  it('does not reject a path that merely starts with an auth-route name', () => {
    // /loginszzz is not /login or under /login/ — it is a legitimate deep link.
    expect(sanitizeReturnTo('/registers-of-deeds')).toBe('/registers-of-deeds');
    expect(sanitizeReturnTo('/setupwizardlike')).toBe('/setupwizardlike');
  });
});

describe('locationToReturnTo', () => {
  it('flattens a router Location into pathname + search + hash', () => {
    expect(
      locationToReturnTo({
        pathname: '/admin/modules',
        search: '?tab=addons',
        hash: '#x',
        state: null,
        key: 'abc'
      })
    ).toBe('/admin/modules?tab=addons#x');
  });

  it('handles a Location with only a pathname', () => {
    expect(locationToReturnTo({ pathname: '/admin/modules' } as never)).toBe(
      '/admin/modules'
    );
  });

  it.each([
    ['null', null],
    ['undefined', undefined],
    ['a string', '/admin/modules'],
    ['an object without pathname', { search: '?x=1' }],
    ['an object with empty pathname', { pathname: '' }]
  ])('returns null for %s', (_label, input) => {
    expect(locationToReturnTo(input as unknown)).toBeNull();
  });
});

describe('DEFAULT_POST_LOGIN', () => {
  it('is a safe in-app path', () => {
    expect(sanitizeReturnTo(DEFAULT_POST_LOGIN)).toBe(DEFAULT_POST_LOGIN);
  });
});
