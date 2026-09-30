import { describe, expect, it } from 'vitest';
import { shouldBlockPolicyNavigation } from './policyNavigation';

const at = (pathname: string, search = '') => ({ pathname, search });

describe('shouldBlockPolicyNavigation', () => {
  it('never blocks a clean form', () => {
    expect(
      shouldBlockPolicyNavigation(
        false,
        at('/admin/compliance/policies/p'),
        at('/admin/compliance')
      )
    ).toBe(false);
  });
  it('blocks leaving the page with unsaved changes', () => {
    expect(
      shouldBlockPolicyNavigation(
        true,
        at('/admin/compliance/policies/p'),
        at('/admin/compliance')
      )
    ).toBe(true);
  });
  it('does not block switching section', () => {
    expect(
      shouldBlockPolicyNavigation(
        true,
        at('/admin/compliance/policies/p', '?section=settings'),
        at('/admin/compliance/policies/p', '?section=history')
      )
    ).toBe(false);
  });
  it('blocks changing the policy a new one is copied from', () => {
    expect(
      shouldBlockPolicyNavigation(
        true,
        at('/admin/compliance/policies/new', '?from=a'),
        at('/admin/compliance/policies/new', '?from=b')
      )
    ).toBe(true);
  });
});
