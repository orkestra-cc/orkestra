import type { TFunction } from 'i18next';
import type { ApiErrorBody } from 'helpers/errorMessage';
import type { PolicyIssue } from 'store/api/complianceApi';

// errorBody extracts the {code, detail} envelope of an RTK Query error.
export const errorBody = (err: unknown): ApiErrorBody | undefined =>
  (err as { data?: ApiErrorBody } | undefined)?.data;

// Paths the backend uses in issues and diffs → i18n keys of their labels.
const FIELD_KEYS: Record<string, string> = {
  name: 'adminCompliance.fields.name',
  description: 'adminCompliance.fields.description',
  'logContent.ipAddress': 'adminCompliance.fields.ipAddress',
  'logContent.userAgent': 'adminCompliance.fields.userAgent',
  'logContent.subjectIds': 'adminCompliance.fields.subjectIds',
  'logContent.piiKeys': 'adminCompliance.fields.piiKeys',
  'logContent.scanFreeText': 'adminCompliance.fields.scanFreeText',
  'accountability.role': 'adminCompliance.fields.role',
  'accountability.ropaRef': 'adminCompliance.fields.ropaRef',
  'accountability.assessmentRef': 'adminCompliance.fields.assessmentRef',
  'accountability.owner': 'adminCompliance.fields.owner',
  'accountability.reviewDueAt': 'adminCompliance.fields.reviewDueAt',
  accountability: 'adminCompliance.fields.accountability'
};

export function fieldLabel(t: TFunction, path: string): string {
  if (path.startsWith('retention.')) {
    return t(
      `adminCompliance.retentionClasses.${path.slice('retention.'.length)}.label`,
      { defaultValue: path }
    );
  }
  if (path.startsWith('sinks')) return t('adminCompliance.fields.sinks');
  const key = FIELD_KEYS[path];
  return key ? t(key) : path;
}

// issueText renders a validation error or warning with its parameters; the
// `fields` parameter (less restrictive fields) is turned into labels.
export function issueText(t: TFunction, issue: PolicyIssue): string {
  const params: Record<string, unknown> = { ...(issue.params ?? {}) };
  if (Array.isArray(params.fields)) {
    params.fields = (params.fields as string[])
      .map(f => fieldLabel(t, f))
      .join(', ');
  }
  return t(`adminCompliance.issues.${issue.code}`, {
    ...params,
    defaultValue: issue.code
  });
}
