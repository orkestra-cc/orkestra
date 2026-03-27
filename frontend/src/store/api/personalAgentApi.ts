import { baseApi } from './baseApi';
import type {
  AgentProject,
  AgentConversation,
  AgentQueryResponse,
  AgentQueryRequest,
  AgentSettings,
} from '../../types/agents';

export const personalAgentApi = baseApi.injectEndpoints({
  endpoints: (builder) => ({
    // Get or auto-create personal agent
    getPersonalAgent: builder.query<AgentProject, void>({
      query: () => '/v1/agents/personal',
      providesTags: ['PersonalAgent'],
    }),

    // Query personal agent
    personalAgentQuery: builder.mutation<AgentQueryResponse, AgentQueryRequest>({
      query: (body) => ({
        url: '/v1/agents/personal/query',
        method: 'POST',
        body,
      }),
      invalidatesTags: ['PersonalConversation'],
    }),

    // Add documents to personal agent scope
    addPersonalDocuments: builder.mutation<AgentProject, { documentUuids: string[] }>({
      query: (body) => ({
        url: '/v1/agents/personal/documents',
        method: 'POST',
        body,
      }),
      invalidatesTags: ['PersonalAgent'],
    }),

    // Remove documents from personal agent scope
    removePersonalDocuments: builder.mutation<AgentProject, { documentUuids: string[] }>({
      query: (body) => ({
        url: '/v1/agents/personal/documents',
        method: 'DELETE',
        body,
      }),
      invalidatesTags: ['PersonalAgent'],
    }),

    // Update personal agent settings
    updatePersonalSettings: builder.mutation<AgentProject, Partial<AgentSettings>>({
      query: (body) => ({
        url: '/v1/agents/personal/settings',
        method: 'PATCH',
        body,
      }),
      invalidatesTags: ['PersonalAgent'],
    }),

    // Get personal agent settings
    getPersonalSettings: builder.query<{ settings: AgentSettings | null }, void>({
      query: () => '/v1/agents/personal/settings',
      providesTags: ['PersonalAgent'],
    }),

    // List personal conversations
    listPersonalConversations: builder.query<
      { conversations: AgentConversation[]; total: number },
      { limit?: number; offset?: number } | void
    >({
      query: (params) =>
        `/v1/agents/personal/conversations?limit=${params?.limit ?? 20}&offset=${params?.offset ?? 0}`,
      providesTags: ['PersonalConversation'],
    }),

    // Get a personal conversation with all messages
    getPersonalConversation: builder.query<AgentConversation, string>({
      query: (uuid) => `/v1/agents/personal/conversations/${uuid}`,
      providesTags: (_result, _err, uuid) => [{ type: 'PersonalConversation', id: uuid }],
    }),

    // Delete a personal conversation
    deletePersonalConversation: builder.mutation<{ message: string }, string>({
      query: (uuid) => ({
        url: `/v1/agents/personal/conversations/${uuid}`,
        method: 'DELETE',
      }),
      invalidatesTags: ['PersonalConversation'],
    }),
  }),
});

export const {
  useGetPersonalAgentQuery,
  usePersonalAgentQueryMutation,
  useAddPersonalDocumentsMutation,
  useRemovePersonalDocumentsMutation,
  useUpdatePersonalSettingsMutation,
  useGetPersonalSettingsQuery,
  useListPersonalConversationsQuery,
  useGetPersonalConversationQuery,
  useDeletePersonalConversationMutation,
} = personalAgentApi;
