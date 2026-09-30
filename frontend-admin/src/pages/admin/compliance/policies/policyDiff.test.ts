import { describe, expect, it } from 'vitest';
import type { PolicyInput } from 'store/api/complianceApi';
import { diffPolicies } from './policyDiff';

const base: PolicyInput = {
  name: 'Strict',
  description: '',
  logContent: {
    ipAddress: 'omitted',
    userAgent: 'full',
    subjectIds: 'uuid',
    piiKeys: ['email', 'phone'],
    scanFreeText: true
  },
  retention: { privileged_change: 400 },
  accountability: {
    role: 'processor',
    ropaRef: 'R-1',
    assessmentRef: 'D-1',
    owner: ''
  }
};

describe('diffPolicies', () => {
  it('returns no rows for identical policies', () => {
    expect(diffPolicies(base, structuredClone(base))).toEqual([]);
  });

  it('lists changed, added and removed fields with placeholders', () => {
    const next = structuredClone(base);
    next.logContent.ipAddress = 'full';
    next.logContent.piiKeys = ['email'];
    next.retention = { admin_access: 400 };
    expect(diffPolicies(base, next)).toEqual([
      { field: 'logContent.ipAddress', before: 'omitted', after: 'full' },
      { field: 'logContent.piiKeys', before: 'email, phone', after: 'email' },
      { field: 'retention.admin_access', before: '—', after: '400' },
      { field: 'retention.privileged_change', before: '400', after: '—' }
    ]);
  });

  it('shows every field of a new policy', () => {
    const rows = diffPolicies(undefined, base);
    expect(rows.find(r => r.field === 'name')).toEqual({
      field: 'name',
      before: '—',
      after: 'Strict'
    });
    expect(rows.every(r => r.before === '—')).toBe(true);
  });
});
