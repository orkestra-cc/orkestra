import { describe, it, expect } from 'vitest';
import { parseOAuthCallback } from './oauthCallbackParams';

const GENERIC = { kind: 'error', errorKey: 'loginFailed' };

describe('parseOAuthCallback (closed contract)', () => {
  it('reads a success outcome with an allowlisted provider from the query', () => {
    for (const p of ['google', 'apple', 'github', 'discord']) {
      expect(parseOAuthCallback(`?provider=${p}&success=true`, '')).toEqual({
        kind: 'success',
        provider: p
      });
    }
  });

  it('refuses a success without an allowlisted provider or with an error', () => {
    expect(parseOAuthCallback('?success=true', '')).toEqual(GENERIC);
    expect(parseOAuthCallback('?success=true&provider=facebook', '')).toEqual(
      GENERIC
    );
    expect(parseOAuthCallback('?success=true&provider=Google', '')).toEqual(
      GENERIC
    );
    expect(
      parseOAuthCallback(
        '?success=true&provider=google&error=oauth_access_denied',
        ''
      )
    ).toEqual(GENERIC);
  });

  it('refuses extra, duplicated or stray keys on either side (exact key sets)', () => {
    // extra query key
    expect(
      parseOAuthCallback('?success=true&provider=google&foo=x', '')
    ).toEqual(GENERIC);
    // duplicated key, even with a consistent second value
    expect(
      parseOAuthCallback('?success=true&success=false&provider=google', '')
    ).toEqual(GENERIC);
    expect(
      parseOAuthCallback('?success=true&success=true&provider=google', '')
    ).toEqual(GENERIC);
    expect(
      parseOAuthCallback('?success=true&provider=google&provider=google', '')
    ).toEqual(GENERIC);
    // a failure may not carry a provider
    expect(
      parseOAuthCallback(
        '?success=false&error=oauth_access_denied&provider=google',
        ''
      )
    ).toEqual(GENERIC);
    // a query outcome may not carry any fragment, however harmless
    expect(
      parseOAuthCallback('?success=true&provider=google', '#foo=x')
    ).toEqual(GENERIC);
    expect(
      parseOAuthCallback('?success=false&error=oauth_access_denied', '#x')
    ).toEqual(GENERIC);
    // an MFA fragment may not carry extra keys — not even a stray access_token
    expect(
      parseOAuthCallback(
        '',
        '#requiresMfa=true&mfaToken=ch-1&webauthnAvailable=false&access_token=x'
      )
    ).toEqual(GENERIC);
    expect(
      parseOAuthCallback(
        '',
        '#requiresMfa=true&mfaToken=ch-1&mfaToken=ch-2&webauthnAvailable=false'
      )
    ).toEqual(GENERIC);
    // an MFA fragment may not carry any query, however harmless
    expect(
      parseOAuthCallback(
        '?foo=x',
        '#requiresMfa=true&mfaToken=ch-1&webauthnAvailable=false'
      )
    ).toEqual(GENERIC);
  });

  it('reads the MFA continuation from the fragment only, with every field explicit', () => {
    expect(
      parseOAuthCallback(
        '',
        '#mfaToken=ch-1&requiresMfa=true&webauthnAvailable=true'
      )
    ).toEqual({
      kind: 'mfa',
      challengeId: 'ch-1',
      webauthnAvailable: true
    });
    expect(
      parseOAuthCallback(
        '',
        '#mfaToken=ch-1&requiresMfa=true&webauthnAvailable=false'
      )
    ).toEqual({
      kind: 'mfa',
      challengeId: 'ch-1',
      webauthnAvailable: false
    });
    // A query cannot smuggle an MFA continuation.
    expect(
      parseOAuthCallback(
        '?requiresMfa=true&mfaToken=ch-1&webauthnAvailable=false',
        ''
      )
    ).toEqual(GENERIC);
  });

  it('refuses an incomplete or malformed MFA fragment', () => {
    // webauthnAvailable missing
    expect(parseOAuthCallback('', '#requiresMfa=true&mfaToken=ch-1')).toEqual(
      GENERIC
    );
    expect(
      parseOAuthCallback(
        '',
        '#requiresMfa=true&mfaToken=ch-1&webauthnAvailable=yes'
      )
    ).toEqual(GENERIC);
    expect(
      parseOAuthCallback(
        '',
        '#requiresMfa=true&mfaToken=&webauthnAvailable=false'
      )
    ).toEqual(GENERIC);
    expect(
      parseOAuthCallback(
        '',
        '#requiresMfa=false&mfaToken=ch-1&webauthnAvailable=false'
      )
    ).toEqual(GENERIC);
    expect(
      parseOAuthCallback('', '#mfaToken=ch-1&webauthnAvailable=false')
    ).toEqual(GENERIC);
  });

  it('refuses an ambiguous payload that mixes an MFA fragment with a query outcome', () => {
    expect(
      parseOAuthCallback(
        '?success=true&provider=google',
        '#requiresMfa=true&mfaToken=ch-1&webauthnAvailable=false'
      )
    ).toEqual(GENERIC);
    expect(
      parseOAuthCallback(
        '?success=false&error=oauth_access_denied',
        '#requiresMfa=true&mfaToken=ch-1&webauthnAvailable=false'
      )
    ).toEqual(GENERIC);
    expect(
      parseOAuthCallback(
        '?provider=google',
        '#requiresMfa=true&mfaToken=ch-1&webauthnAvailable=false'
      )
    ).toEqual(GENERIC);
  });

  it('maps every allowlisted error code to its i18n key', () => {
    const expected: Record<string, string> = {
      oauth_access_denied: 'accessDenied',
      oauth_signup_disabled: 'signupDisabled',
      oauth_link_disabled: 'linkDisabled',
      'auth.oauth_email_unverified': 'emailUnverified',
      oauth_provider_unavailable: 'providerUnavailable',
      oauth_login_failed: 'loginFailed'
    };
    for (const [code, key] of Object.entries(expected)) {
      expect(
        parseOAuthCallback(
          `?success=false&error=${encodeURIComponent(code)}`,
          ''
        )
      ).toEqual({ kind: 'error', errorKey: key });
    }
  });

  it('collapses unknown, empty and hostile codes — and anything else — to the generic key', () => {
    for (const code of [
      '',
      'internal: mongo down',
      '<script>alert(1)</script>',
      'constructor',
      '__proto__',
      'hasOwnProperty'
    ]) {
      expect(
        parseOAuthCallback(
          `?success=false&error=${encodeURIComponent(code)}`,
          ''
        )
      ).toEqual(GENERIC);
    }
    expect(parseOAuthCallback('', '')).toEqual(GENERIC);
    expect(parseOAuthCallback('?success=maybe', '')).toEqual(GENERIC);
    expect(parseOAuthCallback('?provider=google', '')).toEqual(GENERIC);
  });
});
