import { describe, expect, it } from 'vitest';
import type { PolicyInput } from 'store/api/complianceApi';
import {
  copyAsTenantPolicy,
  formFieldOf,
  newTenantPolicy,
  toFormValues,
  toPolicyInput
} from './policyForm';

const tenant: PolicyInput = {
  name: 'Strict',
  description: 'Clinica',
  logContent: {
    ipAddress: 'omitted',
    userAgent: 'full',
    subjectIds: 'hashed',
    piiKeys: ['email', 'phone'],
    scanFreeText: true
  },
  retention: { privileged_change: 400 },
  accountability: {
    role: 'processor',
    ropaRef: 'R-1',
    assessmentRef: 'D-1',
    owner: 'op-1',
    reviewDueAt: '2027-03-30T00:00:00Z'
  }
};

describe('policyForm', () => {
  it('round-trips a policy through the form', () => {
    expect(toPolicyInput(toFormValues(tenant))).toEqual(tenant);
  });

  it('leaves an empty class out (inherited) and parses the PII keys', () => {
    const v = toFormValues(tenant);
    expect(v.retention.admin_access).toBe('');
    v.piiKeys = 'email, phone\nbadge;;';
    const out = toPolicyInput(v);
    expect(out.retention).toEqual({ privileged_change: 400 });
    expect(out.logContent.piiKeys).toEqual(['email', 'phone', 'badge']);
  });

  it('keeps the sinks only when given', () => {
    expect(toPolicyInput(toFormValues(tenant)).sinks).toBeUndefined();
    expect(
      toPolicyInput(toFormValues(tenant), { loki: { days: 14 } }).sinks
    ).toEqual({ loki: { days: 14 } });
  });

  it('starts a tenant policy from the platform content, inheriting every class', () => {
    const p = newTenantPolicy(
      { ...tenant, retention: { admin_access: 365 }, sinks: { loki: {} } },
      new Date('2026-09-30T00:00:00Z')
    );
    expect(p.retention).toEqual({});
    expect(p.sinks).toBeUndefined();
    expect(p.logContent.ipAddress).toBe('omitted');
    expect(p.accountability.reviewDueAt?.slice(0, 10)).toBe('2027-09-30');
  });

  it('copies the platform policy as a tenant policy without platform-only parts', () => {
    const copy = copyAsTenantPolicy(
      {
        ...tenant,
        retention: { admin_access: 365, compliance_evidence: 1825 },
        sinks: { loki: {} }
      },
      'Strict (copy)'
    );
    expect(copy.name).toBe('Strict (copy)');
    expect(copy.retention).toEqual({ admin_access: 365 });
    expect(copy.sinks).toBeUndefined();
  });

  it('maps server fields to form fields', () => {
    expect(formFieldOf('logContent.ipAddress')).toBe('ipAddress');
    expect(formFieldOf('retention.admin_access')).toBe(
      'retention.admin_access'
    );
    expect(formFieldOf('accountability.reviewDueAt')).toBe('reviewDueAt');
    expect(formFieldOf('sinks.loki.days')).toBeUndefined();
  });
});
