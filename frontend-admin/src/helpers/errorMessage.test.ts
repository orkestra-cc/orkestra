import { afterEach, describe, expect, it } from 'vitest';
import i18n from '../i18n';
import { resolveErrorMessage } from './errorMessage';

describe('resolveErrorMessage (ADR-0007)', () => {
  afterEach(() => {
    i18n.removeResourceBundle('en', 'billing');
  });

  it('prefers an addon namespace for a dotted code', () => {
    i18n.addResourceBundle('en', 'billing', {
      errors: { invoice_overdue: 'Invoice overdue' }
    });
    const msg = resolveErrorMessage({
      code: 'billing.invoice_overdue',
      detail: 'raw english'
    });
    expect(msg).toBe('Invoice overdue');
  });

  it('falls back to the core errors namespace when no addon ns matches', () => {
    // errors.401 ships in the core en.json.
    expect(resolveErrorMessage({ code: '401', detail: 'x' })).not.toBe('x');
    expect(resolveErrorMessage({ code: '401', detail: 'x' })).not.toBe('401');
  });

  it('falls back to the backend detail when no translation exists', () => {
    expect(
      resolveErrorMessage({
        code: 'billing.unknown_code',
        detail: 'Server says'
      })
    ).toBe('Server says');
  });

  it('uses the explicit fallback, then the raw code, last', () => {
    expect(resolveErrorMessage({ code: 'x.y' }, 'fallback')).toBe('fallback');
    expect(resolveErrorMessage({ code: 'x.y' })).toBe('x.y');
    expect(resolveErrorMessage(undefined, 'only')).toBe('only');
    expect(resolveErrorMessage(null)).toBe('');
  });
});
