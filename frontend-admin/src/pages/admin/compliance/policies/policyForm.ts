import * as yup from 'yup';
import type {
  AccountabilityRole,
  IPAddressMode,
  PolicyInput,
  RetentionClassKey,
  SinkPolicy,
  SubjectIDMode,
  UserAgentMode
} from 'store/api/complianceApi';

export const RETENTION_CLASSES: RetentionClassKey[] = [
  'admin_access',
  'privileged_change',
  'client_activity',
  'authentication_security',
  'compliance_evidence'
];
// Mirror of retentionclass.Class.TenantSettable; the server validates anyway.
export const TENANT_SETTABLE_CLASSES: RetentionClassKey[] = [
  'admin_access',
  'privileged_change',
  'client_activity'
];
export const IP_MODES: IPAddressMode[] = [
  'full',
  'truncated',
  'hashed',
  'omitted'
];
export const UA_MODES: UserAgentMode[] = ['full', 'omitted'];
export const SUBJECT_MODES: SubjectIDMode[] = ['uuid', 'hashed', 'omitted'];
export const ROLES: AccountabilityRole[] = [
  'controller',
  'processor',
  'joint_controller'
];

// PolicyFormValues is the flat editor state: every input is a string (an
// empty retention class means "inherited from the platform").
export interface PolicyFormValues {
  name: string;
  description: string;
  ipAddress: IPAddressMode;
  userAgent: UserAgentMode;
  subjectIds: SubjectIDMode;
  piiKeys: string;
  scanFreeText: boolean;
  retention: Record<RetentionClassKey, string>;
  role: AccountabilityRole;
  ropaRef: string;
  assessmentRef: string;
  owner: string;
  reviewDueAt: string;
}

export function toFormValues(p: PolicyInput): PolicyFormValues {
  const retention = {} as Record<RetentionClassKey, string>;
  for (const c of RETENTION_CLASSES) {
    const days = p.retention[c];
    retention[c] = days === undefined ? '' : String(days);
  }
  return {
    name: p.name,
    description: p.description ?? '',
    ipAddress: p.logContent.ipAddress,
    userAgent: p.logContent.userAgent,
    subjectIds: p.logContent.subjectIds,
    piiKeys: p.logContent.piiKeys.join(', '),
    scanFreeText: p.logContent.scanFreeText,
    retention,
    role: p.accountability.role,
    ropaRef: p.accountability.ropaRef,
    assessmentRef: p.accountability.assessmentRef,
    owner: p.accountability.owner,
    reviewDueAt: p.accountability.reviewDueAt
      ? p.accountability.reviewDueAt.slice(0, 10)
      : ''
  };
}

export function toPolicyInput(
  v: PolicyFormValues,
  sinks?: SinkPolicy | null
): PolicyInput {
  const retention: Partial<Record<RetentionClassKey, number>> = {};
  for (const c of RETENTION_CLASSES) {
    const raw = (v.retention[c] ?? '').trim();
    if (raw !== '') retention[c] = Number(raw);
  }
  const input: PolicyInput = {
    name: v.name.trim(),
    description: v.description.trim(),
    logContent: {
      ipAddress: v.ipAddress,
      userAgent: v.userAgent,
      subjectIds: v.subjectIds,
      piiKeys: v.piiKeys
        .split(/[\s,;]+/)
        .map(k => k.trim())
        .filter(Boolean),
      scanFreeText: v.scanFreeText
    },
    retention,
    accountability: {
      role: v.role,
      ropaRef: v.ropaRef.trim(),
      assessmentRef: v.assessmentRef.trim(),
      owner: v.owner.trim(),
      ...(v.reviewDueAt ? { reviewDueAt: `${v.reviewDueAt}T00:00:00Z` } : {})
    }
  };
  if (sinks) input.sinks = sinks;
  return input;
}

// A new tenant policy starts from the platform's log content, inherits every
// retention class and is due for review in one year.
export function newTenantPolicy(
  platform: PolicyInput,
  now = new Date()
): PolicyInput {
  const due = new Date(now);
  due.setUTCFullYear(due.getUTCFullYear() + 1);
  return {
    name: '',
    description: '',
    logContent: {
      ...platform.logContent,
      piiKeys: [...platform.logContent.piiKeys]
    },
    retention: {},
    accountability: {
      role: 'processor',
      ropaRef: '',
      assessmentRef: '',
      owner: '',
      reviewDueAt: due.toISOString()
    }
  };
}

// copyAsTenantPolicy duplicates a policy (the platform one included) as a
// tenant policy: no sinks, only the tenant-settable classes.
export function copyAsTenantPolicy(p: PolicyInput, name: string): PolicyInput {
  const retention: Partial<Record<RetentionClassKey, number>> = {};
  for (const c of TENANT_SETTABLE_CLASSES) {
    if (p.retention[c] !== undefined) retention[c] = p.retention[c];
  }
  return {
    name,
    description: p.description,
    logContent: { ...p.logContent, piiKeys: [...p.logContent.piiKeys] },
    retention,
    accountability: { ...p.accountability }
  };
}

type FormPath = keyof PolicyFormValues | `retention.${RetentionClassKey}`;

const SERVER_FIELDS: Record<string, keyof PolicyFormValues> = {
  name: 'name',
  description: 'description',
  'logContent.ipAddress': 'ipAddress',
  'logContent.userAgent': 'userAgent',
  'logContent.subjectIds': 'subjectIds',
  'logContent.piiKeys': 'piiKeys',
  'logContent.scanFreeText': 'scanFreeText',
  'accountability.role': 'role',
  'accountability.ropaRef': 'ropaRef',
  'accountability.assessmentRef': 'assessmentRef',
  'accountability.owner': 'owner',
  'accountability.reviewDueAt': 'reviewDueAt'
};

// formFieldOf maps an issue's server field to the form field that shows it.
export function formFieldOf(serverField?: string): FormPath | undefined {
  if (!serverField) return undefined;
  if (serverField.startsWith('retention.')) {
    const cls = serverField.slice('retention.'.length) as RetentionClassKey;
    return RETENTION_CLASSES.includes(cls) ? `retention.${cls}` : undefined;
  }
  return SERVER_FIELDS[serverField];
}

const days = yup.string().defined().matches(/^\d*$/);

// Client-side checks are shape-only; the server owns the rules (minimums,
// classes a tenant may set) and returns them through the validate endpoint.
export const policySchema: yup.ObjectSchema<PolicyFormValues> = yup.object({
  name: yup.string().trim().required().max(80),
  description: yup.string().defined().max(500),
  ipAddress: yup.mixed<IPAddressMode>().oneOf(IP_MODES).required(),
  userAgent: yup.mixed<UserAgentMode>().oneOf(UA_MODES).required(),
  subjectIds: yup.mixed<SubjectIDMode>().oneOf(SUBJECT_MODES).required(),
  piiKeys: yup.string().defined(),
  scanFreeText: yup.boolean().required(),
  retention: yup
    .object({
      admin_access: days,
      privileged_change: days,
      client_activity: days,
      authentication_security: days,
      compliance_evidence: days
    })
    .required(),
  role: yup.mixed<AccountabilityRole>().oneOf(ROLES).required(),
  ropaRef: yup.string().defined().max(200),
  assessmentRef: yup.string().defined().max(200),
  owner: yup.string().defined().max(64),
  reviewDueAt: yup
    .string()
    .defined()
    .matches(/^(\d{4}-\d{2}-\d{2})?$/)
});
