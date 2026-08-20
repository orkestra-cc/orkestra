import { baseApi } from './baseApi';
import { skipToken } from '@reduxjs/toolkit/query';
import type {
  LogLevelsView,
  LogPreviewFilters,
  LogPreviewResponse,
  PermanentLogLevelsInput,
  SetLevelBody,
  StartDiagnosticInput,
  StopDiagnosticInput
} from '../../types/observability';

// observabilityApi wraps the platform-admin endpoints for runtime log-
// level mutation (ADR-0005 Phase F). Administrator-only on the backend.
// All mutations return the fresh LogLevelsView so the table re-renders
// without a separate refetch — the backend View() is cheap (in-memory).

export const observabilityApi = baseApi.injectEndpoints({
  endpoints: builder => ({
    getLogLevels: builder.query<LogLevelsView, void>({
      query: () => ({
        url: '/v1/admin/observability/log-levels',
        method: 'GET'
      }),
      providesTags: [{ type: 'LogLevels' as const, id: 'SNAPSHOT' }]
    }),

    applyPermanentLogLevels: builder.mutation<
      LogLevelsView,
      PermanentLogLevelsInput
    >({
      query: body => ({
        url: '/v1/admin/observability/log-levels',
        method: 'PUT',
        body
      }),
      invalidatesTags: [{ type: 'LogLevels' as const, id: 'SNAPSHOT' }]
    }),

    startDiagnostic: builder.mutation<LogLevelsView, StartDiagnosticInput>({
      query: ({ module, level, durationMinutes }) => ({
        url: `/v1/admin/observability/log-levels/${encodeURIComponent(module)}/diagnostic`,
        method: 'PUT',
        body:
          durationMinutes === undefined ? { level } : { level, durationMinutes }
      }),
      invalidatesTags: [{ type: 'LogLevels' as const, id: 'SNAPSHOT' }]
    }),

    stopDiagnostic: builder.mutation<LogLevelsView, StopDiagnosticInput>({
      query: ({ module }) => ({
        url: `/v1/admin/observability/log-levels/${encodeURIComponent(module)}/diagnostic`,
        method: 'DELETE'
      }),
      invalidatesTags: [{ type: 'LogLevels' as const, id: 'SNAPSHOT' }]
    }),

    getLogPreview: builder.query<LogPreviewResponse, LogPreviewFilters>({
      query: ({ module, windowMinutes, level, q, limit }) => {
        const params = new URLSearchParams({
          module,
          windowMinutes: String(windowMinutes)
        });
        if (level !== undefined) params.set('level', level);
        if (q !== undefined) params.set('q', q);
        if (limit !== undefined) params.set('limit', String(limit));
        return {
          url: `/v1/admin/observability/log-levels/logs?${params.toString()}`,
          method: 'GET'
        };
      }
    }),

    setGlobalLogLevel: builder.mutation<LogLevelsView, SetLevelBody>({
      query: body => ({
        url: '/v1/admin/observability/log-levels/global',
        method: 'PUT',
        body
      }),
      invalidatesTags: [{ type: 'LogLevels' as const, id: 'SNAPSHOT' }]
    }),

    setModuleLogLevel: builder.mutation<
      LogLevelsView,
      { module: string } & SetLevelBody
    >({
      query: ({ module, level }) => ({
        url: `/v1/admin/observability/log-levels/${encodeURIComponent(module)}`,
        method: 'PUT',
        body: { level }
      }),
      invalidatesTags: [{ type: 'LogLevels' as const, id: 'SNAPSHOT' }]
    }),

    unsetModuleLogLevel: builder.mutation<LogLevelsView, { module: string }>({
      query: ({ module }) => ({
        url: `/v1/admin/observability/log-levels/${encodeURIComponent(module)}`,
        method: 'DELETE'
      }),
      invalidatesTags: [{ type: 'LogLevels' as const, id: 'SNAPSHOT' }]
    }),

    resetLogLevels: builder.mutation<LogLevelsView, void>({
      query: () => ({
        url: '/v1/admin/observability/log-levels/reset',
        method: 'POST'
      }),
      invalidatesTags: [{ type: 'LogLevels' as const, id: 'SNAPSHOT' }]
    })
  })
});

export const {
  useGetLogLevelsQuery,
  useApplyPermanentLogLevelsMutation,
  useStartDiagnosticMutation,
  useStopDiagnosticMutation,
  useGetLogPreviewQuery: useGetLogPreviewQueryBase,
  useSetGlobalLogLevelMutation,
  useSetModuleLogLevelMutation,
  useUnsetModuleLogLevelMutation,
  useResetLogLevelsMutation
} = observabilityApi;

// A module selection is required by the backend. Keep the hook uninitialized
// while the workspace has no selected module instead of issuing a known-invalid
// request with an empty query parameter.
export const useGetLogPreviewQuery = (
  filters: LogPreviewFilters | undefined,
  options?: Parameters<typeof useGetLogPreviewQueryBase>[1]
) => useGetLogPreviewQueryBase(filters?.module ? filters : skipToken, options);
