import { baseApi } from './baseApi';

// complianceApi wraps the core compliance module's admin surface (ADR-0009):
// the GDPR DSR workflow (erasure requests), legal holds, retention preview,
// and the audit-event trail. Destructive mutations (execute erasure, place /
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
        url: `/v1/admin/compliance/erasure-requests/${encodeURIComponent(id)}/execute`,
        method: 'POST',
        body: { mode }
      }),
      invalidatesTags: [{ type: 'ErasureRequest' as const, id: 'LIST' }]
    }),
    rejectErasureRequest: builder.mutation<void, { id: string; note?: string }>(
      {
        query: ({ id, note }) => ({
          url: `/v1/admin/compliance/erasure-requests/${encodeURIComponent(id)}/reject`,
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
  useRetentionPreviewQuery
} = complianceApi;
