import { baseApi } from './baseApi';
import type {
  Job,
  JobPreviewInput,
  JobPreviewResult,
  JobRunsArgs,
  JobRunsPage,
  UpdateJobInput
} from '../../types/jobs';

// jobsApi wraps the Tier-1 jobs admin API. Reads need system.jobs.read, the
// three mutations system.jobs.manage. Every write carries the job revision it
// read; a 409 jobs.revision_conflict is handled by the caller (refetch).
//
// invalidatesTags returns [] on error: RTK Query invalidates tags on rejected
// mutations too, which would refetch the list behind a failed write.

const LIST = { type: 'Jobs' as const, id: 'LIST' };

export const jobsApi = baseApi.injectEndpoints({
  endpoints: builder => ({
    listJobs: builder.query<Job[], void>({
      query: () => ({ url: '/v1/admin/jobs', method: 'GET' }),
      transformResponse: (r: { jobs: Job[] }) => r.jobs ?? [],
      providesTags: [LIST]
    }),

    listJobRuns: builder.query<JobRunsPage, JobRunsArgs>({
      query: ({ key, status, cursor, limit }) => ({
        url: `/v1/admin/jobs/${encodeURIComponent(key)}/runs`,
        method: 'GET',
        params: { status, cursor, limit }
      }),
      providesTags: (_r, _e, { key }) => [{ type: 'JobRuns', id: key }]
    }),

    previewJobSchedule: builder.query<JobPreviewResult, JobPreviewInput>({
      query: body => ({
        url: '/v1/admin/jobs/schedule-preview',
        method: 'POST',
        body
      }),
      keepUnusedDataFor: 30
    }),

    updateJob: builder.mutation<Job, UpdateJobInput>({
      query: ({ key, ...body }) => ({
        url: `/v1/admin/jobs/${encodeURIComponent(key)}`,
        method: 'PATCH',
        body
      }),
      invalidatesTags: (_r, error) => (error ? [] : [LIST])
    }),

    clearJobOverride: builder.mutation<Job, { key: string; revision: number }>({
      query: ({ key, revision }) => ({
        url: `/v1/admin/jobs/${encodeURIComponent(key)}/override`,
        method: 'DELETE',
        params: { revision }
      }),
      invalidatesTags: (_r, error) => (error ? [] : [LIST])
    }),

    runJobNow: builder.mutation<Job, { key: string }>({
      query: ({ key }) => ({
        url: `/v1/admin/jobs/${encodeURIComponent(key)}/run`,
        method: 'POST'
      }),
      invalidatesTags: (_r, error, { key }) =>
        error ? [] : [LIST, { type: 'JobRuns' as const, id: key }]
    })
  })
});

export const {
  useListJobsQuery,
  useListJobRunsQuery,
  useLazyListJobRunsQuery,
  usePreviewJobScheduleQuery,
  useUpdateJobMutation,
  useClearJobOverrideMutation,
  useRunJobNowMutation
} = jobsApi;
