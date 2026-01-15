import { baseApi } from './baseApi';

// Crane types based on backend OpenAPI
export interface CraneResponse {
  id: string;
  nome: string;
  tipo: string;
  matricola: string;
  verificareSuMezzo?: string;
  vehicleId?: string;
  scadenzaVerifica?: string;
  note?: string;
  isActive: boolean;
  createdAt: string;
  updatedAt: string;
}

export interface CraneListResponse {
  cranes: CraneResponse[];
  total: number;
  page: number;
  pageSize: number;
  totalPages: number;
}

export interface CreateCraneInput {
  nome: string;
  tipo: string;
  matricola: string;
  verificareSuMezzo?: string;
  scadenzaVerifica?: string;
  note?: string;
}

export interface UpdateCraneInput {
  nome?: string;
  tipo?: string;
  matricola?: string;
  verificareSuMezzo?: string;
  scadenzaVerifica?: string;
  note?: string;
  isActive?: boolean;
}

export interface DeleteCraneResponse {
  message: string;
}

export interface CraneListParams {
  tipo?: string;
  verificareSuMezzo?: string;
  isActive?: boolean;
  search?: string;
  verificaProssimaGiorni?: number;
  page?: number;
  pageSize?: number;
}

// Crane management API slice
export const craneApi = baseApi.injectEndpoints({
  endpoints: (builder) => ({
    // List cranes with filtering and pagination
    getCranes: builder.query<CraneListResponse, CraneListParams | undefined>({
      query: (params) => {
        const searchParams = new URLSearchParams();

        // Add parameters if they exist
        if (params?.tipo) searchParams.append('tipo', params.tipo);
        if (params?.verificareSuMezzo) searchParams.append('verificareSuMezzo', params.verificareSuMezzo);
        if (params?.isActive !== undefined) searchParams.append('isActive', String(params.isActive));
        if (params?.search) searchParams.append('search', params.search);
        if (params?.verificaProssimaGiorni !== undefined) searchParams.append('verificaProssimaGiorni', String(params.verificaProssimaGiorni));
        if (params?.page !== undefined) searchParams.append('page', String(params.page));
        if (params?.pageSize !== undefined) searchParams.append('pageSize', String(params.pageSize));

        return {
          url: `/api/v1/cranes?${searchParams.toString()}`,
          method: 'GET',
        };
      },
      providesTags: (result) =>
        result
          ? [
              ...result.cranes.map(({ id }) => ({ type: 'Crane' as const, id })),
              { type: 'Crane', id: 'LIST' },
            ]
          : [{ type: 'Crane', id: 'LIST' }],
    }),

    // Get crane by ID
    getCraneById: builder.query<CraneResponse, string>({
      query: (id) => `/api/v1/cranes/${id}`,
      providesTags: (_result, _error, id) => [{ type: 'Crane', id }],
    }),

    // Create new crane
    createCrane: builder.mutation<CraneResponse, CreateCraneInput>({
      query: (craneData) => ({
        url: '/api/v1/cranes',
        method: 'POST',
        body: craneData,
      }),
      invalidatesTags: [{ type: 'Crane', id: 'LIST' }],
    }),

    // Update crane
    updateCrane: builder.mutation<CraneResponse, { id: string; data: UpdateCraneInput }>({
      query: ({ id, data }) => ({
        url: `/api/v1/cranes/${id}`,
        method: 'PUT',
        body: data,
      }),
      invalidatesTags: (_result, _error, { id }) => [
        { type: 'Crane', id },
        { type: 'Crane', id: 'LIST' },
      ],
    }),

    // Delete crane
    deleteCrane: builder.mutation<DeleteCraneResponse, string>({
      query: (id) => ({
        url: `/api/v1/cranes/${id}`,
        method: 'DELETE',
      }),
      invalidatesTags: (_result, _error, id) => [
        { type: 'Crane', id },
        { type: 'Crane', id: 'LIST' },
      ],
    }),
  }),
});

// Export hooks for usage in components
export const {
  useGetCranesQuery,
  useGetCraneByIdQuery,
  useCreateCraneMutation,
  useUpdateCraneMutation,
  useDeleteCraneMutation,
} = craneApi;