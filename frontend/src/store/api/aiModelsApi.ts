import { baseApi } from './baseApi';
import type {
  AIModelConfig,
  CreateAIModelRequest,
  UpdateAIModelRequest,
  TestModelResult,
  QuickPromptResult,
  AvailableModel,
} from '../../types/aiModels';

export const aiModelsApi = baseApi.injectEndpoints({
  endpoints: (builder) => ({
    listAIModels: builder.query<
      { models: AIModelConfig[] },
      { type?: string; provider?: string; category?: string } | void
    >({
      query: (params) => {
        const searchParams = new URLSearchParams();
        if (params?.type) searchParams.append('type', params.type);
        if (params?.provider) searchParams.append('provider', params.provider);
        if (params?.category) searchParams.append('category', params.category);
        const qs = searchParams.toString();
        return `/v1/ai/models${qs ? `?${qs}` : ''}`;
      },
      providesTags: ['AIModel'],
    }),

    getAIModel: builder.query<AIModelConfig, string>({
      query: (uuid) => `/v1/ai/models/${uuid}`,
      providesTags: ['AIModel'],
    }),

    createAIModel: builder.mutation<AIModelConfig, CreateAIModelRequest>({
      query: (body) => ({
        url: '/v1/ai/models',
        method: 'POST',
        body,
      }),
      invalidatesTags: ['AIModel'],
    }),

    updateAIModel: builder.mutation<AIModelConfig, { uuid: string; body: UpdateAIModelRequest }>({
      query: ({ uuid, body }) => ({
        url: `/v1/ai/models/${uuid}`,
        method: 'PATCH',
        body,
      }),
      invalidatesTags: ['AIModel'],
    }),

    deleteAIModel: builder.mutation<{ message: string }, string>({
      query: (uuid) => ({
        url: `/v1/ai/models/${uuid}`,
        method: 'DELETE',
      }),
      invalidatesTags: ['AIModel'],
    }),

    setDefaultAIModel: builder.mutation<{ message: string }, string>({
      query: (uuid) => ({
        url: `/v1/ai/models/${uuid}/default`,
        method: 'POST',
      }),
      invalidatesTags: ['AIModel'],
    }),

    testAIModel: builder.mutation<TestModelResult, string>({
      query: (uuid) => ({
        url: `/v1/ai/models/${uuid}/test`,
        method: 'POST',
      }),
    }),

    quickPromptAIModel: builder.mutation<QuickPromptResult, { uuid: string; prompt: string }>({
      query: ({ uuid, prompt }) => ({
        url: `/v1/ai/models/${uuid}/prompt`,
        method: 'POST',
        body: { prompt },
      }),
    }),

    fetchAIProviderModels: builder.mutation<
      { models: AvailableModel[] },
      { provider: string; baseUrl: string; apiKey?: string; modelType?: string }
    >({
      query: (body) => ({
        url: '/v1/ai/models/fetch',
        method: 'POST',
        body,
      }),
    }),
  }),
});

export const {
  useListAIModelsQuery,
  useGetAIModelQuery,
  useCreateAIModelMutation,
  useUpdateAIModelMutation,
  useDeleteAIModelMutation,
  useSetDefaultAIModelMutation,
  useTestAIModelMutation,
  useQuickPromptAIModelMutation,
  useFetchAIProviderModelsMutation,
} = aiModelsApi;
