import { baseApi } from './baseApi';

// --- Types ---

interface SkillRequest {
  url: string;
  locale?: string;
  context?: string;
}

interface SkillTaskPollResponse {
  taskId: string;
  status: 'running' | 'completed' | 'failed';
  skill?: string;
  result?: any;
  inputTokens?: number;
  outputTokens?: number;
  latencyMs?: number;
  modelUsed?: string;
  error?: string;
}

interface ProspectRequest {
  url: string;
  locale?: string;
}

interface ProspectJobResponse {
  jobId: string;
  streamUrl: string;
}

interface QuickProspectResponse {
  score: number;
  grade: string;
  companyName: string;
  summary: string;
  findings: any;
  inputTokens: number;
  outputTokens: number;
  latencyMs: number;
}

interface Job {
  uuid: string;
  createdBy: string;
  companyUrl: string;
  locale: string;
  status: string;
  phases: Array<{ name: string; status: string; startedAt?: string; completedAt?: string }>;
  agentResults?: any[];
  totalScore?: number;
  grade?: string;
  errorMessage?: string;
  reportUuid?: string;
  createdAt: string;
  updatedAt: string;
  completedAt?: string;
}

interface JobListResponse {
  jobs: Job[];
  total: number;
  page: number;
  pageSize: number;
}

// --- API Slice ---

export const salesApi = baseApi.injectEndpoints({
  endpoints: (builder) => ({
    // Individual skills (async: POST returns taskId, then poll)
    submitSkill: builder.mutation<{ taskId: string }, { skill: string } & SkillRequest>({
      query: ({ skill, ...body }) => ({
        url: `v1/sales/${skill}`,
        method: 'POST',
        body,
      }),
    }),

    pollSkillTask: builder.query<SkillTaskPollResponse, string>({
      query: (taskId) => `v1/sales/skills/${taskId}`,
    }),

    // Full async prospect
    createProspectJob: builder.mutation<ProspectJobResponse, ProspectRequest>({
      query: (body) => ({
        url: 'v1/sales/prospect',
        method: 'POST',
        body,
      }),
      invalidatesTags: ['Sales'],
    }),

    // Quick sync prospect
    runQuickProspect: builder.mutation<QuickProspectResponse, ProspectRequest>({
      query: (body) => ({
        url: 'v1/sales/prospect/quick',
        method: 'POST',
        body,
      }),
    }),

    // Jobs
    listSalesJobs: builder.query<JobListResponse, { page?: number; pageSize?: number; status?: string }>({
      query: (params) => ({
        url: 'v1/sales/jobs',
        params,
      }),
      providesTags: ['Sales'],
    }),

    getSalesJob: builder.query<Job, string>({
      query: (uuid) => `v1/sales/jobs/${uuid}`,
      providesTags: ['Sales'],
    }),

    cancelSalesJob: builder.mutation<void, string>({
      query: (uuid) => ({
        url: `v1/sales/jobs/${uuid}`,
        method: 'DELETE',
      }),
      invalidatesTags: ['Sales'],
    }),

    rerunSalesJobAgents: builder.mutation<Job, string>({
      query: (uuid) => ({
        url: `v1/sales/jobs/${uuid}/rerun`,
        method: 'POST',
      }),
      invalidatesTags: ['Sales'],
    }),

    retrySalesJob: builder.mutation<{ jobId: string; streamUrl: string }, string>({
      query: (uuid) => ({
        url: `v1/sales/jobs/${uuid}/retry`,
        method: 'POST',
      }),
      invalidatesTags: ['Sales'],
    }),

    // Reports
    listSalesReports: builder.query<ReportListResponse, { page?: number; pageSize?: number }>({
      query: (params) => ({
        url: 'v1/sales/reports',
        params,
      }),
      providesTags: ['Sales'],
    }),

    getSalesReport: builder.query<Report, string>({
      query: (uuid) => `v1/sales/reports/${uuid}`,
      providesTags: ['Sales'],
    }),

    // Prompts
    listSalesPrompts: builder.query<{ prompts: SalesPromptConfig[] }, { category?: string }>({
      query: (params) => ({
        url: 'v1/sales/prompts',
        params,
      }),
      providesTags: ['Sales'],
    }),

    getSalesPrompt: builder.query<SalesPromptConfig, string>({
      query: (uuid) => `v1/sales/prompts/${uuid}`,
      providesTags: ['Sales'],
    }),

    updateSalesPrompt: builder.mutation<SalesPromptConfig, { uuid: string; content: string; displayName?: string; description?: string }>({
      query: ({ uuid, ...body }) => ({
        url: `v1/sales/prompts/${uuid}`,
        method: 'PATCH',
        body,
      }),
      invalidatesTags: ['Sales'],
    }),

    resetSalesPrompt: builder.mutation<SalesPromptConfig, string>({
      query: (uuid) => ({
        url: `v1/sales/prompts/${uuid}/reset`,
        method: 'POST',
      }),
      invalidatesTags: ['Sales'],
    }),

    generateSalesReport: builder.mutation<Report, string>({
      query: (jobUuid) => ({
        url: `v1/sales/reports/generate/${jobUuid}`,
        method: 'POST',
      }),
      invalidatesTags: ['Sales'],
    }),

    deleteSalesReport: builder.mutation<void, string>({
      query: (uuid) => ({
        url: `v1/sales/reports/${uuid}`,
        method: 'DELETE',
      }),
      invalidatesTags: ['Sales'],
    }),

    // Settings
    getSalesSettings: builder.query<SalesSettings, void>({
      query: () => 'v1/sales/settings',
      providesTags: ['Sales'],
    }),

    updateSalesSettings: builder.mutation<SalesSettings, Partial<SalesSettings>>({
      query: (body) => ({
        url: 'v1/sales/settings',
        method: 'PATCH',
        body,
      }),
      invalidatesTags: ['Sales'],
    }),
  }),
});

export interface SalesPromptConfig {
  uuid: string;
  category: string;
  name: string;
  displayName: string;
  description: string;
  content: string;
  isCustom: boolean;
  createdAt: string;
  updatedAt: string;
}

export interface Report {
  uuid: string;
  jobUuid: string;
  createdBy: string;
  companyUrl: string;
  companyName: string;
  score: number;
  grade: string;
  contentMd?: string;
  agentData?: any;
  createdAt: string;
}

export interface ReportListResponse {
  reports: Report[];
  total: number;
  page: number;
  pageSize: number;
}

export interface SalesSettings {
  uuid: string;
  userUuid: string;
  modelUuid?: string;
  temperature?: number;
  maxTokens?: number;
  locale?: string;
  batchMode?: boolean;
  createdAt?: string;
  updatedAt?: string;
}

export const {
  useSubmitSkillMutation,
  useLazyPollSkillTaskQuery,
  useCreateProspectJobMutation,
  useRunQuickProspectMutation,
  useListSalesJobsQuery,
  useGetSalesJobQuery,
  useCancelSalesJobMutation,
  useRerunSalesJobAgentsMutation,
  useRetrySalesJobMutation,
  useListSalesReportsQuery,
  useGetSalesReportQuery,
  useGenerateSalesReportMutation,
  useListSalesPromptsQuery,
  useGetSalesPromptQuery,
  useUpdateSalesPromptMutation,
  useResetSalesPromptMutation,
  useDeleteSalesReportMutation,
  useGetSalesSettingsQuery,
  useUpdateSalesSettingsMutation,
} = salesApi;
