import { baseApi } from './baseApi';
import type {
  AgentProject,
  AgentConversation,
  AgentQueryResponse,
  AgentQueryRequest,
  AgentSettings,
  CreateProjectRequest,
  UpdateProjectRequest,
} from '../../types/agents';

export const agentsApi = baseApi.injectEndpoints({
  endpoints: (builder) => ({
    // --- Project CRUD ---

    listProjects: builder.query<{ projects: AgentProject[] }, { status?: string } | void>({
      query: (params) => {
        const searchParams = new URLSearchParams();
        if (params?.status) searchParams.append('status', params.status);
        const qs = searchParams.toString();
        return `/v1/agents/projects${qs ? `?${qs}` : ''}`;
      },
      providesTags: ['AgentProject'],
    }),

    getProject: builder.query<AgentProject, string>({
      query: (uuid) => `/v1/agents/projects/${uuid}`,
      providesTags: (_result, _err, uuid) => [{ type: 'AgentProject', id: uuid }],
    }),

    createProject: builder.mutation<AgentProject, CreateProjectRequest>({
      query: (body) => ({
        url: '/v1/agents/projects',
        method: 'POST',
        body,
      }),
      invalidatesTags: ['AgentProject'],
    }),

    updateProject: builder.mutation<AgentProject, { uuid: string; body: UpdateProjectRequest }>({
      query: ({ uuid, body }) => ({
        url: `/v1/agents/projects/${uuid}`,
        method: 'PATCH',
        body,
      }),
      invalidatesTags: (_result, _err, { uuid }) => [
        'AgentProject',
        { type: 'AgentProject', id: uuid },
      ],
    }),

    deleteProject: builder.mutation<{ message: string }, string>({
      query: (uuid) => ({
        url: `/v1/agents/projects/${uuid}`,
        method: 'DELETE',
      }),
      invalidatesTags: ['AgentProject'],
    }),

    // --- Document Scoping ---

    addProjectDocuments: builder.mutation<AgentProject, { uuid: string; documentUuids: string[] }>({
      query: ({ uuid, documentUuids }) => ({
        url: `/v1/agents/projects/${uuid}/documents`,
        method: 'POST',
        body: { documentUuids },
      }),
      invalidatesTags: (_result, _err, { uuid }) => [
        'AgentProject',
        { type: 'AgentProject', id: uuid },
      ],
    }),

    removeProjectDocuments: builder.mutation<AgentProject, { uuid: string; documentUuids: string[] }>({
      query: ({ uuid, documentUuids }) => ({
        url: `/v1/agents/projects/${uuid}/documents`,
        method: 'DELETE',
        body: { documentUuids },
      }),
      invalidatesTags: (_result, _err, { uuid }) => [
        'AgentProject',
        { type: 'AgentProject', id: uuid },
      ],
    }),

    updateProjectFilters: builder.mutation<AgentProject, { uuid: string; isoStandards?: string[]; categories?: string[] }>({
      query: ({ uuid, ...body }) => ({
        url: `/v1/agents/projects/${uuid}/filters`,
        method: 'PATCH',
        body,
      }),
      invalidatesTags: (_result, _err, { uuid }) => [{ type: 'AgentProject', id: uuid }],
    }),

    // --- Agent Query ---

    agentQuery: builder.mutation<AgentQueryResponse, { projectUuid: string; body: AgentQueryRequest }>({
      query: ({ projectUuid, body }) => ({
        url: `/v1/agents/projects/${projectUuid}/query`,
        method: 'POST',
        body,
      }),
      invalidatesTags: ['AgentConversation'],
    }),

    // --- Conversations ---

    createConversation: builder.mutation<AgentConversation, { projectUuid: string; persona?: string }>({
      query: ({ projectUuid, persona }) => ({
        url: `/v1/agents/projects/${projectUuid}/conversations`,
        method: 'POST',
        body: { persona },
      }),
      invalidatesTags: ['AgentConversation'],
    }),

    listConversations: builder.query<
      { conversations: AgentConversation[]; total: number },
      { projectUuid: string; limit?: number; offset?: number }
    >({
      query: ({ projectUuid, limit = 20, offset = 0 }) =>
        `/v1/agents/projects/${projectUuid}/conversations?limit=${limit}&offset=${offset}`,
      providesTags: ['AgentConversation'],
    }),

    getConversation: builder.query<AgentConversation, string>({
      query: (uuid) => `/v1/agents/conversations/${uuid}`,
      providesTags: (_result, _err, uuid) => [{ type: 'AgentConversation', id: uuid }],
    }),

    deleteConversation: builder.mutation<{ message: string }, string>({
      query: (uuid) => ({
        url: `/v1/agents/conversations/${uuid}`,
        method: 'DELETE',
      }),
      invalidatesTags: ['AgentConversation'],
    }),

    // --- Admin ---

    // --- Settings ---

    getProjectSettings: builder.query<{ settings: AgentSettings | null }, string>({
      query: (uuid) => `/v1/agents/projects/${uuid}/settings`,
      providesTags: (_result, _err, uuid) => [{ type: 'AgentProject', id: `settings-${uuid}` }],
    }),

    updateProjectSettings: builder.mutation<AgentProject, { uuid: string; settings: Partial<AgentSettings> }>({
      query: ({ uuid, settings }) => ({
        url: `/v1/agents/projects/${uuid}/settings`,
        method: 'PATCH',
        body: settings,
      }),
      invalidatesTags: (_result, _err, { uuid }) => [
        { type: 'AgentProject', id: uuid },
        { type: 'AgentProject', id: `settings-${uuid}` },
      ],
    }),

    agentHealthCheck: builder.query<{ hindsight: string }, void>({
      query: () => '/v1/agents/health',
    }),
  }),
});

export const {
  useListProjectsQuery,
  useGetProjectQuery,
  useCreateProjectMutation,
  useUpdateProjectMutation,
  useDeleteProjectMutation,
  useAddProjectDocumentsMutation,
  useRemoveProjectDocumentsMutation,
  useUpdateProjectFiltersMutation,
  useAgentQueryMutation,
  useCreateConversationMutation,
  useListConversationsQuery,
  useGetConversationQuery,
  useDeleteConversationMutation,
  useGetProjectSettingsQuery,
  useUpdateProjectSettingsMutation,
  useAgentHealthCheckQuery,
} = agentsApi;
