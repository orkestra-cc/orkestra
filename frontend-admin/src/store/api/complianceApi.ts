import { baseApi } from './baseApi';
import type {
  DsrEraseResponse,
  DsrExportResponse,
  ListAuditEventsParams,
  ListAuditEventsResponse,
  Soc2Evidence,
} from '../../types/compliance';

// complianceApi wraps the platform-admin compliance endpoints. The backend
// gates every call behind `system.compliance.audit.read` (super_admin /
// administrator / developer inherit it). Endpoints here are the thin
// read-only projection — writes happen inside other modules via the
// iface.AuditSink backend pathway, not from the client.

export const complianceApi = baseApi.injectEndpoints({
  endpoints: (builder) => ({
    listAuditEvents: builder.query<ListAuditEventsResponse, ListAuditEventsParams | void>({
      query: (params) => {
        const clean = params
          ? Object.fromEntries(
              Object.entries(params).filter(
                ([, value]) => value !== undefined && value !== '' && value !== null,
              ),
            )
          : undefined;
        return {
          url: '/v1/admin/audit-events',
          method: 'GET',
          params: clean,
        };
      },
      providesTags: (result) =>
        result
          ? [
              { type: 'AuditEvent' as const, id: 'LIST' },
              ...result.items.map((ev) => ({ type: 'AuditEvent' as const, id: ev.uuid })),
            ]
          : [{ type: 'AuditEvent' as const, id: 'LIST' }],
    }),

    getSoc2Evidence: builder.query<Soc2Evidence, void>({
      query: () => ({ url: '/v1/admin/compliance/soc2/evidence', method: 'GET' }),
      // The backend recomputes from source on every call; no persisted
      // snapshot exists in v1. Tag for the "Regenerate" button path so
      // invalidating Soc2Evidence forces a fresh fetch.
      providesTags: [{ type: 'Soc2Evidence' as const, id: 'SNAPSHOT' }],
    }),

    // DSR — data subject rights. Both endpoints derive their subject from
    // the caller's JWT (see me_handler.go) so no body is needed.
    exportMyData: builder.mutation<DsrExportResponse, void>({
      query: () => ({ url: '/v1/me/dsr/export', method: 'POST' }),
    }),
    eraseMyData: builder.mutation<DsrEraseResponse, void>({
      query: () => ({ url: '/v1/me/dsr/erase', method: 'POST' }),
    }),
  }),
  overrideExisting: false,
});

export const {
  useListAuditEventsQuery,
  useGetSoc2EvidenceQuery,
  useExportMyDataMutation,
  useEraseMyDataMutation,
} = complianceApi;
