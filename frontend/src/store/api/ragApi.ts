import { baseApi } from './baseApi';
import type {
  RagDocument,
  RagChunk,
  UpdateDocumentRequest,
  RagQueryRequest,
  RagQueryResponse,
  RelationshipTypeConfig,
  CreateRelationshipTypeRequest,
  UpdateRelationshipTypeRequest,
  DocumentRelationsResponse,
} from '../../types/rag';

export const ragApi = baseApi.injectEndpoints({
  endpoints: (builder) => ({
    // --- Documents ---

    uploadDocument: builder.mutation<RagDocument, FormData>({
      query: (formData) => ({
        url: '/v1/rag/documents',
        method: 'POST',
        body: formData,
        formData: true,
      }),
      invalidatesTags: ['RagDocument', 'GraphSchema'],
    }),

    listDocuments: builder.query<{ documents: RagDocument[] }, { status?: string; isoStandard?: string } | void>({
      query: (params) => {
        const searchParams = new URLSearchParams();
        if (params?.status) searchParams.append('status', params.status);
        if (params?.isoStandard) searchParams.append('isoStandard', params.isoStandard);
        const qs = searchParams.toString();
        return `/v1/rag/documents${qs ? `?${qs}` : ''}`;
      },
      providesTags: ['RagDocument'],
    }),

    getDocument: builder.query<RagDocument, string>({
      query: (uuid) => `/v1/rag/documents/${uuid}`,
      providesTags: ['RagDocument'],
    }),

    updateDocument: builder.mutation<RagDocument, { uuid: string; data: UpdateDocumentRequest }>({
      query: ({ uuid, data }) => ({
        url: `/v1/rag/documents/${uuid}`,
        method: 'PATCH',
        body: data,
      }),
      invalidatesTags: ['RagDocument'],
    }),

    getDocumentChunks: builder.query<{ chunks: RagChunk[] }, string>({
      query: (uuid) => `/v1/rag/documents/${uuid}/chunks`,
      providesTags: ['RagDocument'],
    }),

    deleteDocument: builder.mutation<{ message: string }, string>({
      query: (uuid) => ({
        url: `/v1/rag/documents/${uuid}`,
        method: 'DELETE',
      }),
      invalidatesTags: ['RagDocument', 'GraphSchema'],
    }),

    // --- Relationship Types ---

    listRelationshipTypes: builder.query<{ relationshipTypes: RelationshipTypeConfig[] }, void>({
      query: () => '/v1/rag/relationships',
      providesTags: ['RagRelationship'],
    }),

    createRelationshipType: builder.mutation<RelationshipTypeConfig, CreateRelationshipTypeRequest>({
      query: (body) => ({
        url: '/v1/rag/relationships',
        method: 'POST',
        body,
      }),
      invalidatesTags: ['RagRelationship'],
    }),

    updateRelationshipType: builder.mutation<RelationshipTypeConfig, { uuid: string; data: UpdateRelationshipTypeRequest }>({
      query: ({ uuid, data }) => ({
        url: `/v1/rag/relationships/${uuid}`,
        method: 'PATCH',
        body: data,
      }),
      invalidatesTags: ['RagRelationship'],
    }),

    deleteRelationshipType: builder.mutation<{ message: string }, string>({
      query: (uuid) => ({
        url: `/v1/rag/relationships/${uuid}`,
        method: 'DELETE',
      }),
      invalidatesTags: ['RagRelationship'],
    }),

    // --- Cross-Document Relations ---

    getDocumentRelations: builder.query<DocumentRelationsResponse, string>({
      query: (uuid) => `/v1/rag/documents/${uuid}/relations`,
      providesTags: (_result, _err, uuid) => [{ type: 'RagDocument', id: `relations-${uuid}` }],
    }),

    // --- RAG Query ---

    ragQuery: builder.mutation<RagQueryResponse, RagQueryRequest>({
      query: (body) => ({
        url: '/v1/rag/query',
        method: 'POST',
        body,
      }),
    }),
  }),
});

export const {
  useUploadDocumentMutation,
  useListDocumentsQuery,
  useGetDocumentQuery,
  useUpdateDocumentMutation,
  useGetDocumentChunksQuery,
  useDeleteDocumentMutation,
  useListRelationshipTypesQuery,
  useCreateRelationshipTypeMutation,
  useUpdateRelationshipTypeMutation,
  useDeleteRelationshipTypeMutation,
  useGetDocumentRelationsQuery,
  useRagQueryMutation,
} = ragApi;
