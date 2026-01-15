import { baseApi } from './baseApi';

// Tachograph types based on backend OpenAPI
export interface TachographResponse {
  id: string;
  nome: string;
  targa: string;
  vehicleId?: string;
  luogo?: string;
  note?: string;
  revisioneProgrammata?: string;
  scadenzaRevisione?: string;
  isActive: boolean;
  createdAt: string;
  updatedAt: string;
}

export interface TachographListResponse {
  tachographs: TachographResponse[];
  total: number;
  page: number;
  pageSize: number;
  totalPages: number;
}

export interface CreateTachographInput {
  nome: string;
  targa: string;
  luogo?: string;
  note?: string;
  revisioneProgrammata?: string;
  scadenzaRevisione?: string;
}

export interface UpdateTachographInput {
  nome?: string;
  targa?: string;
  luogo?: string;
  note?: string;
  revisioneProgrammata?: string;
  scadenzaRevisione?: string;
  isActive?: boolean;
}

export interface DeleteTachographResponse {
  message: string;
}

export interface TachographListParams {
  isActive?: boolean;
  search?: string;
  revisioneProssimaGiorni?: number;
  page?: number;
  pageSize?: number;
}

// Tachograph management API slice
export const tachographApi = baseApi.injectEndpoints({
  endpoints: (builder) => ({
    // List tachographs with filtering and pagination
    getTachographs: builder.query<TachographListResponse, TachographListParams | undefined>({
      query: (params) => {
        const searchParams = new URLSearchParams();

        // Add parameters if they exist
        if (params?.isActive !== undefined) searchParams.append('isActive', String(params.isActive));
        if (params?.search) searchParams.append('search', params.search);
        if (params?.revisioneProssimaGiorni !== undefined) searchParams.append('revisioneProssimaGiorni', String(params.revisioneProssimaGiorni));
        if (params?.page !== undefined) searchParams.append('page', String(params.page));
        if (params?.pageSize !== undefined) searchParams.append('pageSize', String(params.pageSize));

        return {
          url: `/api/v1/tachographs?${searchParams.toString()}`,
          method: 'GET',
        };
      },
      providesTags: (result) =>
        result
          ? [
              ...result.tachographs.map(({ id }) => ({ type: 'Tachograph' as const, id })),
              { type: 'Tachograph', id: 'LIST' },
            ]
          : [{ type: 'Tachograph', id: 'LIST' }],
    }),

    // Get tachograph by ID
    getTachographById: builder.query<TachographResponse, string>({
      query: (id) => `/api/v1/tachographs/${id}`,
      providesTags: (_result, _error, id) => [{ type: 'Tachograph', id }],
    }),

    // Create new tachograph
    createTachograph: builder.mutation<TachographResponse, CreateTachographInput>({
      query: (data) => ({
        url: '/api/v1/tachographs',
        method: 'POST',
        body: data,
      }),
      invalidatesTags: [{ type: 'Tachograph', id: 'LIST' }],
    }),

    // Update tachograph
    updateTachograph: builder.mutation<TachographResponse, { id: string; data: UpdateTachographInput }>({
      query: ({ id, data }) => ({
        url: `/api/v1/tachographs/${id}`,
        method: 'PUT',
        body: data,
      }),
      invalidatesTags: (_result, _error, { id }) => [
        { type: 'Tachograph', id },
        { type: 'Tachograph', id: 'LIST' },
      ],
    }),

    // Delete tachograph (soft delete)
    deleteTachograph: builder.mutation<DeleteTachographResponse, string>({
      query: (id) => ({
        url: `/api/v1/tachographs/${id}`,
        method: 'DELETE',
      }),
      invalidatesTags: (_result, _error, id) => [
        { type: 'Tachograph', id },
        { type: 'Tachograph', id: 'LIST' },
      ],
    }),
  }),
});

// Export hooks for usage in functional components
export const {
  useGetTachographsQuery,
  useGetTachographByIdQuery,
  useCreateTachographMutation,
  useUpdateTachographMutation,
  useDeleteTachographMutation,
} = tachographApi;

// Export the API endpoints for use in other slices
export const { endpoints: tachographEndpoints } = tachographApi;