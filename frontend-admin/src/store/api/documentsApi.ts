import { baseApi } from './baseApi';
import type {
  Template,
  TemplateListResponse,
  TemplateListParams,
  CreateTemplateInput,
  UpdateTemplateInput,
  DuplicateTemplateInput,
  GeneratePDFInput,
  PreviewHTMLInput,
  PreviewHTMLFromContentInput,
  PreviewHTMLResponse,
  GeneratedDocumentMeta,
  ServiceStatusResponse,
  TemplateVariablesResponse,
  TemplateType
} from '../../types/documents';

// API slice for documents module
export const documentsApi = baseApi.injectEndpoints({
  endpoints: builder => ({
    // ============================================
    // Template Endpoints
    // ============================================

    // Get all templates with optional filters
    getTemplates: builder.query<TemplateListResponse, TemplateListParams>({
      query: (params = {}) => {
        const queryParams = new URLSearchParams();
        if (params.page) queryParams.append('page', params.page.toString());
        if (params.pageSize)
          queryParams.append('pageSize', params.pageSize.toString());
        if (params.type) queryParams.append('type', params.type);
        if (params.isDefault !== undefined)
          queryParams.append('isDefault', params.isDefault.toString());
        if (params.isBuiltIn !== undefined)
          queryParams.append('isBuiltIn', params.isBuiltIn.toString());
        if (params.isActive !== undefined)
          queryParams.append('isActive', params.isActive.toString());
        if (params.search) queryParams.append('search', params.search);

        const queryString = queryParams.toString();
        return {
          url: `/v1/documents/templates${queryString ? `?${queryString}` : ''}`,
          method: 'GET'
        };
      },
      providesTags: result =>
        result?.templates
          ? [
              ...result.templates.map(({ id }) => ({
                type: 'DocumentTemplate' as const,
                id
              })),
              { type: 'DocumentTemplate' as const, id: 'LIST' }
            ]
          : [{ type: 'DocumentTemplate' as const, id: 'LIST' }]
    }),

    // Get a single template by ID
    // Note: Huma v2 returns the template directly at root level, not wrapped in {template: ...}
    getTemplate: builder.query<Template, string>({
      query: id => ({
        url: `/v1/documents/templates/${id}`,
        method: 'GET'
      }),
      providesTags: (_result, _error, id) => [
        { type: 'DocumentTemplate' as const, id }
      ]
    }),

    // Get template variables for a type
    // Note: Huma v2 returns the response directly at root level
    getTemplateVariables: builder.query<
      TemplateVariablesResponse,
      TemplateType
    >({
      query: type => ({
        url: `/v1/documents/templates/variables/${type}`,
        method: 'GET'
      })
    }),

    // Create a new template
    // Note: Huma v2 returns the template directly at root level
    createTemplate: builder.mutation<Template, CreateTemplateInput>({
      query: data => ({
        url: '/v1/documents/templates',
        method: 'POST',
        body: { template: data }
      }),
      invalidatesTags: [{ type: 'DocumentTemplate', id: 'LIST' }]
    }),

    // Update an existing template
    // Note: Huma v2 expects body fields directly, not wrapped in "template"
    updateTemplate: builder.mutation<
      Template,
      { id: string; data: UpdateTemplateInput }
    >({
      query: ({ id, data }) => ({
        url: `/v1/documents/templates/${id}`,
        method: 'PATCH',
        body: data
      }),
      invalidatesTags: (_result, _error, { id }) => [
        { type: 'DocumentTemplate', id },
        { type: 'DocumentTemplate', id: 'LIST' }
      ]
    }),

    // Delete a template
    deleteTemplate: builder.mutation<
      { success: boolean; message: string },
      string
    >({
      query: id => ({
        url: `/v1/documents/templates/${id}`,
        method: 'DELETE'
      }),
      invalidatesTags: (_result, _error, id) => [
        { type: 'DocumentTemplate', id },
        { type: 'DocumentTemplate', id: 'LIST' }
      ]
    }),

    // Set a template as default
    setDefaultTemplate: builder.mutation<
      { success: boolean; message: string },
      string
    >({
      query: id => ({
        url: `/v1/documents/templates/${id}/default`,
        method: 'POST'
      }),
      invalidatesTags: [{ type: 'DocumentTemplate', id: 'LIST' }]
    }),

    // Duplicate a template
    // Note: Huma v2 returns the template directly at root level
    duplicateTemplate: builder.mutation<
      Template,
      { id: string; data: DuplicateTemplateInput }
    >({
      query: ({ id, data }) => ({
        url: `/v1/documents/templates/${id}/duplicate`,
        method: 'POST',
        body: data
      }),
      invalidatesTags: [{ type: 'DocumentTemplate', id: 'LIST' }]
    }),

    // ============================================
    // PDF Generation Endpoints
    // ============================================

    // Generate a PDF
    // Note: Huma v2 returns the document metadata directly at root level
    generatePDF: builder.mutation<GeneratedDocumentMeta, GeneratePDFInput>({
      query: data => ({
        url: '/v1/documents/generate',
        method: 'POST',
        body: { input: data }
      }),
      invalidatesTags: [{ type: 'GeneratedDocument', id: 'LIST' }]
    }),

    // Preview HTML from template
    previewHTML: builder.mutation<string, PreviewHTMLInput>({
      query: data => ({
        url: '/v1/documents/preview',
        method: 'POST',
        body: { input: data }
      }),
      transformResponse: (response: PreviewHTMLResponse) => response.html
    }),

    // Preview HTML from raw content
    previewHTMLFromContent: builder.mutation<
      string,
      PreviewHTMLFromContentInput
    >({
      query: data => ({
        url: '/v1/documents/preview/content',
        method: 'POST',
        body: { input: data }
      }),
      transformResponse: (response: PreviewHTMLResponse) => response.html
    }),

    // Get document metadata
    // Note: Huma v2 returns the document metadata directly at root level
    getDocument: builder.query<GeneratedDocumentMeta, string>({
      query: id => ({
        url: `/v1/documents/${id}`,
        method: 'GET'
      }),
      providesTags: (_result, _error, id) => [
        { type: 'GeneratedDocument' as const, id }
      ]
    }),

    // Get service status
    getDocumentsServiceStatus: builder.query<ServiceStatusResponse, void>({
      query: () => ({
        url: '/v1/documents/status',
        method: 'GET'
      })
    })
  })
});

// Export auto-generated hooks
export const {
  // Template queries
  useGetTemplatesQuery,
  useGetTemplateQuery,
  useGetTemplateVariablesQuery,
  useLazyGetTemplatesQuery,
  useLazyGetTemplateQuery,

  // Template mutations
  useCreateTemplateMutation,
  useUpdateTemplateMutation,
  useDeleteTemplateMutation,
  useSetDefaultTemplateMutation,
  useDuplicateTemplateMutation,

  // PDF generation
  useGeneratePDFMutation,
  usePreviewHTMLMutation,
  usePreviewHTMLFromContentMutation,

  // Document queries
  useGetDocumentQuery,
  useGetDocumentsServiceStatusQuery
} = documentsApi;
