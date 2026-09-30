import { baseApi } from './baseApi';

// complianceApi wraps the core compliance module's admin surface (ADR-0009):
// the GDPR DSR workflow (erasure requests), legal holds, retention preview,
// the audit-event trail, and the policy engine (catalog, assignments,
// four-eyes change requests). Destructive mutations (execute erasure, place /
// release hold) are step-up-gated on the backend; baseApi's 401 interceptor
// drives the StepUpModal + replays automatically, so callers just `.unwrap()`.

export interface AuditEvent {
  uuid: string;
  tenantId?: string;
  actorUserId?: string;
  actorEmail?: string;
  actorType: string;
  action: string;
  resourceType?: string;
  resourceId?: string;
  outcome: string;
  ipAddress?: string;
  timestamp: string;
}
export interface AuditEventsResponse {
  items: AuditEvent[];
  total: number;
  limit: number;
  offset: number;
}

export interface ErasureRequest {
  uuid: string;
  userUuid: string;
  tenantId?: string;
  reason?: string;
  status: string;
  requestedAt: string;
  resolvedAt?: string;
  resolvedBy?: string;
  mode?: string;
}

export interface LegalHold {
  uuid: string;
  userUuid: string;
  tenantId?: string;
  reason: string;
  caseRef?: string;
  placedBy: string;
  placedAt: string;
  active: boolean;
}

export interface RetentionPreview {
  cutoff: string;
  count: number;
  userUuids: string[];
}

// SOC2Evidence is the point-in-time control snapshot from
// GET /v1/admin/compliance/soc2/evidence. `summary` is a flat metric→count
// map; `controls` keys are SOC2 CC codes (e.g. "CC6.1_logical_access") whose
// values are nested attribute objects. Both are open maps — the page renders
// known keys with friendly labels and falls back to the raw key otherwise.
export interface SOC2Evidence {
  generatedAt: string;
  summary: Record<string, number>;
  controls: Record<string, Record<string, unknown>>;
}

// --- Policy engine (compliance spec §1, §7) ---------------------------------

export type IPAddressMode = 'full' | 'truncated' | 'hashed' | 'omitted';
export type UserAgentMode = 'full' | 'omitted';
export type SubjectIDMode = 'uuid' | 'hashed' | 'omitted';
export type RetentionClassKey =
  | 'admin_access'
  | 'privileged_change'
  | 'client_activity'
  | 'authentication_security'
  | 'compliance_evidence';
export type AccountabilityRole =
  'controller' | 'processor' | 'joint_controller';

export interface LogContentPolicy {
  ipAddress: IPAddressMode;
  userAgent: UserAgentMode;
  subjectIds: SubjectIDMode;
  piiKeys: string[];
  scanFreeText: boolean;
}

export interface Accountability {
  role: AccountabilityRole;
  ropaRef: string;
  assessmentRef: string;
  owner: string;
  reviewDueAt?: string;
}

// The sink section is edited in a later stage; the console sends it back
// untouched on the platform policy.
export type SinkPolicy = Record<string, unknown>;

export interface PolicyInput {
  name: string;
  description: string;
  logContent: LogContentPolicy;
  retention: Partial<Record<RetentionClassKey, number>>;
  sinks?: SinkPolicy | null;
  accountability: Accountability;
}

export interface CompliancePolicy extends PolicyInput {
  uuid: string;
  isPlatformDefault: boolean;
  version: number;
  createdBy: string;
  updatedBy: string;
  createdAt: string;
  updatedAt: string;
  changeReason: string;
}

export interface CompliancePolicyView extends CompliancePolicy {
  assignedTenants: number;
  lessRestrictiveFields: string[];
  reviewOverdue: boolean;
}

export interface PolicyIssue {
  code: string;
  field?: string;
  params?: Record<string, unknown>;
}

export interface ValidationResult {
  errors: PolicyIssue[];
  warnings: PolicyIssue[];
}

export interface PolicyVersion {
  policyUuid: string;
  version: number;
  changeKind: 'create' | 'update' | 'delete';
  snapshot: CompliancePolicy;
  changedBy: string;
  changedAt: string;
  reason: string;
  changeRequestUuid?: string;
  approvedBy?: string;
}

export interface PolicyAssignmentView {
  tenantId: string;
  tenantKind: 'internal' | 'external';
  policyUuid: string;
  assignedBy: string;
  assignedAt: string;
  reason: string;
  tenantName?: string;
  policyName?: string;
}

export interface PolicyDetail {
  policy: CompliancePolicy;
  assignments: PolicyAssignmentView[];
}

export type ChangeRequestStatus =
  'pending' | 'approved' | 'rejected' | 'superseded' | 'expired';
export type ChangeRequestFilter = ChangeRequestStatus | 'all';

export interface ChangeRequest {
  uuid: string;
  kind: 'create' | 'update' | 'assign' | 'unassign';
  policyUuid?: string;
  tenantId?: string;
  payload: {
    policy?: PolicyInput;
    tenantId?: string;
    policyUuid?: string;
    previousPolicyUuid?: string;
  };
  expectedVersion: number;
  warnings: string[];
  reason: string;
  requestedBy: string;
  requestedAt: string;
  status: ChangeRequestStatus;
  decidedBy?: string;
  decidedAt?: string;
  decisionNote?: string;
}

export interface WriteResponse {
  applied: boolean;
  policy?: CompliancePolicy;
  assignment?: PolicyAssignmentView;
  changeRequest?: ChangeRequest;
  warnings: PolicyIssue[];
}

export interface RetentionClassInfo {
  key: RetentionClassKey;
  events: string;
  purpose: string;
  legalBasis: string;
  minDays: number;
  hardMinimum: boolean;
  defaultDays: number;
  tenantSettable: boolean;
}

export interface RetentionDecision {
  class: RetentionClassKey;
  policyUuid: string;
  policyVersion: number;
  days: number;
}

export interface EffectivePolicy {
  tenantId: string;
  source: 'assigned' | 'platform';
  policy: CompliancePolicy;
  assignment?: PolicyAssignmentView;
  retention: RetentionDecision[];
}

// Every policy-engine write can change policies, assignments and requests.
const policyEngineTags = [
  'CompliancePolicy',
  'CompliancePolicyAssignment',
  'ComplianceChangeRequest'
] as const;

export const complianceApi = baseApi.injectEndpoints({
  endpoints: builder => ({
    listAuditEvents: builder.query<
      AuditEventsResponse,
      { actionPrefix?: string; limit?: number } | void
    >({
      query: params => {
        const sp = new URLSearchParams();
        if (params && params.actionPrefix)
          sp.set('actionPrefix', params.actionPrefix);
        sp.set('limit', String((params && params.limit) || 50));
        return {
          url: `/v1/admin/audit-events?${sp.toString()}`,
          method: 'GET'
        };
      },
      providesTags: [{ type: 'AuditEvent' as const, id: 'LIST' }]
    }),

    listErasureRequests: builder.query<{ items: ErasureRequest[] }, void>({
      query: () => ({
        url: '/v1/admin/compliance/erasure-requests',
        method: 'GET'
      }),
      providesTags: [{ type: 'ErasureRequest' as const, id: 'LIST' }]
    }),
    executeErasureRequest: builder.mutation<
      { purged: Record<string, unknown> },
      { id: string; mode: string }
    >({
      query: ({ id, mode }) => ({
        url: `/v1/admin/compliance/erasure-requests/${encodeURIComponent(
          id
        )}/execute`,
        method: 'POST',
        body: { mode }
      }),
      invalidatesTags: [{ type: 'ErasureRequest' as const, id: 'LIST' }]
    }),
    rejectErasureRequest: builder.mutation<void, { id: string; note?: string }>(
      {
        query: ({ id, note }) => ({
          url: `/v1/admin/compliance/erasure-requests/${encodeURIComponent(
            id
          )}/reject`,
          method: 'POST',
          body: { note }
        }),
        invalidatesTags: [{ type: 'ErasureRequest' as const, id: 'LIST' }]
      }
    ),

    listLegalHolds: builder.query<{ items: LegalHold[] }, void>({
      query: () => ({ url: '/v1/admin/compliance/legal-holds', method: 'GET' }),
      providesTags: [{ type: 'LegalHold' as const, id: 'LIST' }]
    }),
    placeLegalHold: builder.mutation<
      LegalHold,
      { userUuid: string; reason: string; caseRef?: string }
    >({
      query: body => ({
        url: '/v1/admin/compliance/legal-holds',
        method: 'POST',
        body
      }),
      invalidatesTags: [{ type: 'LegalHold' as const, id: 'LIST' }]
    }),
    releaseLegalHold: builder.mutation<
      void,
      { id: string; releaseReason?: string }
    >({
      query: ({ id, releaseReason }) => ({
        url: `/v1/admin/compliance/legal-holds/${encodeURIComponent(id)}`,
        method: 'DELETE',
        body: { releaseReason }
      }),
      invalidatesTags: [{ type: 'LegalHold' as const, id: 'LIST' }]
    }),

    retentionPreview: builder.query<RetentionPreview, void>({
      query: () => ({
        url: '/v1/admin/compliance/retention/preview',
        method: 'GET'
      })
    }),

    // SOC2 evidence snapshot — recomputed server-side on each call (idempotent).
    // 404s when the compliance.soc2_enabled sub-feature is off.
    soc2Evidence: builder.query<SOC2Evidence, void>({
      query: () => ({
        url: '/v1/admin/compliance/soc2/evidence',
        method: 'GET'
      })
    }),

    // Policy engine: catalog, assignments and four-eyes change requests.
    listCompliancePolicies: builder.query<
      { items: CompliancePolicyView[] },
      void
    >({
      query: () => ({ url: '/v1/admin/compliance/policies', method: 'GET' }),
      providesTags: ['CompliancePolicy']
    }),
    getCompliancePolicy: builder.query<PolicyDetail, string>({
      query: id => ({
        url: `/v1/admin/compliance/policies/${encodeURIComponent(id)}`,
        method: 'GET'
      }),
      providesTags: ['CompliancePolicy', 'CompliancePolicyAssignment']
    }),
    listPolicyVersions: builder.query<{ items: PolicyVersion[] }, string>({
      query: id => ({
        url: `/v1/admin/compliance/policies/${encodeURIComponent(id)}/versions`,
        method: 'GET'
      }),
      providesTags: ['CompliancePolicy']
    }),
    getEffectivePolicy: builder.query<EffectivePolicy, string>({
      query: tenantId => ({
        url: `/v1/admin/compliance/policies/effective?tenantId=${encodeURIComponent(tenantId)}`,
        method: 'GET'
      }),
      providesTags: ['CompliancePolicy', 'CompliancePolicyAssignment']
    }),
    listRetentionClasses: builder.query<{ items: RetentionClassInfo[] }, void>({
      query: () => ({
        url: '/v1/admin/compliance/retention-classes',
        method: 'GET'
      })
    }),
    validatePolicy: builder.mutation<
      ValidationResult,
      { policyId?: string; policy: PolicyInput }
    >({
      query: body => ({
        url: '/v1/admin/compliance/policies/validate',
        method: 'POST',
        body
      })
    }),
    createPolicy: builder.mutation<
      WriteResponse,
      { policy: PolicyInput; reason: string; acknowledgeWarnings: boolean }
    >({
      query: body => ({
        url: '/v1/admin/compliance/policies',
        method: 'POST',
        body
      }),
      invalidatesTags: [...policyEngineTags]
    }),
    updatePolicy: builder.mutation<
      WriteResponse,
      {
        id: string;
        expectedVersion: number;
        policy: PolicyInput;
        reason: string;
        acknowledgeWarnings: boolean;
      }
    >({
      query: ({ id, ...body }) => ({
        url: `/v1/admin/compliance/policies/${encodeURIComponent(id)}`,
        method: 'PUT',
        body
      }),
      invalidatesTags: [...policyEngineTags]
    }),
    deletePolicy: builder.mutation<
      void,
      { id: string; expectedVersion: number; reason: string }
    >({
      query: ({ id, expectedVersion, reason }) => ({
        url: `/v1/admin/compliance/policies/${encodeURIComponent(id)}?expectedVersion=${expectedVersion}`,
        method: 'DELETE',
        body: { reason }
      }),
      invalidatesTags: [...policyEngineTags]
    }),
    listPolicyAssignments: builder.query<
      { items: PolicyAssignmentView[] },
      void
    >({
      query: () => ({
        url: '/v1/admin/compliance/policy-assignments',
        method: 'GET'
      }),
      providesTags: ['CompliancePolicyAssignment']
    }),
    validateAssignment: builder.mutation<
      ValidationResult,
      { tenantId: string; policyId?: string }
    >({
      query: ({ tenantId, policyId }) => ({
        url: `/v1/admin/compliance/policy-assignments/${encodeURIComponent(tenantId)}/validate`,
        method: 'POST',
        body: { policyId }
      })
    }),
    assignPolicy: builder.mutation<
      WriteResponse,
      {
        tenantId: string;
        policyId: string;
        reason: string;
        acknowledgeWarnings: boolean;
      }
    >({
      query: ({ tenantId, ...body }) => ({
        url: `/v1/admin/compliance/policy-assignments/${encodeURIComponent(tenantId)}`,
        method: 'PUT',
        body
      }),
      invalidatesTags: [...policyEngineTags]
    }),
    unassignPolicy: builder.mutation<
      WriteResponse,
      { tenantId: string; reason: string; acknowledgeWarnings: boolean }
    >({
      query: ({ tenantId, ...body }) => ({
        url: `/v1/admin/compliance/policy-assignments/${encodeURIComponent(tenantId)}`,
        method: 'DELETE',
        body
      }),
      invalidatesTags: [...policyEngineTags]
    }),
    listChangeRequests: builder.query<
      { items: ChangeRequest[] },
      ChangeRequestFilter
    >({
      query: status => ({
        url:
          status === 'all'
            ? '/v1/admin/compliance/change-requests'
            : `/v1/admin/compliance/change-requests?status=${encodeURIComponent(status)}`,
        method: 'GET'
      }),
      providesTags: ['ComplianceChangeRequest']
    }),
    approveChangeRequest: builder.mutation<
      WriteResponse,
      { id: string; note: string }
    >({
      query: ({ id, note }) => ({
        url: `/v1/admin/compliance/change-requests/${encodeURIComponent(id)}/approve`,
        method: 'POST',
        body: { note }
      }),
      invalidatesTags: [...policyEngineTags]
    }),
    rejectChangeRequest: builder.mutation<
      ChangeRequest,
      { id: string; note: string }
    >({
      query: ({ id, note }) => ({
        url: `/v1/admin/compliance/change-requests/${encodeURIComponent(id)}/reject`,
        method: 'POST',
        body: { note }
      }),
      invalidatesTags: ['ComplianceChangeRequest']
    })
  })
});

export const {
  useListAuditEventsQuery,
  useListErasureRequestsQuery,
  useExecuteErasureRequestMutation,
  useRejectErasureRequestMutation,
  useListLegalHoldsQuery,
  usePlaceLegalHoldMutation,
  useReleaseLegalHoldMutation,
  useRetentionPreviewQuery,
  useSoc2EvidenceQuery,
  useListCompliancePoliciesQuery,
  useGetCompliancePolicyQuery,
  useListPolicyVersionsQuery,
  useGetEffectivePolicyQuery,
  useListRetentionClassesQuery,
  useValidatePolicyMutation,
  useCreatePolicyMutation,
  useUpdatePolicyMutation,
  useDeletePolicyMutation,
  useListPolicyAssignmentsQuery,
  useValidateAssignmentMutation,
  useAssignPolicyMutation,
  useUnassignPolicyMutation,
  useListChangeRequestsQuery,
  useApproveChangeRequestMutation,
  useRejectChangeRequestMutation
} = complianceApi;
