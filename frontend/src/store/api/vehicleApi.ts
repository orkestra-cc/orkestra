import { baseApi } from './baseApi';

// Vehicle types based on backend OpenAPI
export interface VehicleResponse {
  id: string;
  nome: string;
  targa: string;
  tipo: string; // 'motrice' | 'rimorchio' | 'semi-rimorchio' | 'trattore' | 'semovente'
  luogo?: string;
  note?: string;
  revisioneProgrammata?: string;
  scadenzaRevisione?: string;
  insuranceExpiry?: string;
  carTaxExpiry?: string;
  isActive: boolean;
  createdAt: string;
  updatedAt: string;
}

export interface VehicleListResponse {
  vehicles: VehicleResponse[];
  total: number;
  page: number;
  pageSize: number;
  totalPages: number;
}

export interface CreateVehicleInput {
  nome: string;
  targa: string;
  tipo: string;
  luogo?: string;
  note?: string;
  revisioneProgrammata?: string;
  scadenzaRevisione?: string;
  insuranceExpiry?: string;
  carTaxExpiry?: string;
}

export interface UpdateVehicleInput {
  nome?: string;
  targa?: string;
  tipo?: string;
  luogo?: string;
  note?: string;
  revisioneProgrammata?: string;
  scadenzaRevisione?: string;
  insuranceExpiry?: string;
  carTaxExpiry?: string;
  isActive?: boolean;
}

export interface DeleteVehicleResponse {
  message: string;
}

export interface GetExpiringVehiclesResponse {
  vehicles: VehicleResponse[];
}

export interface VehicleListParams {
  tipo?: string;
  isActive?: boolean;
  search?: string;
  revisioneProssimaGiorni?: number;
  page?: number;
  pageSize?: number;
}

// Vehicle management API slice
export const vehicleApi = baseApi.injectEndpoints({
  endpoints: (builder) => ({
    // List vehicles with filtering and pagination
    getVehicles: builder.query<VehicleListResponse, VehicleListParams | undefined>({
      query: (params) => {
        const searchParams = new URLSearchParams();

        // Add parameters if they exist
        if (params?.tipo) searchParams.append('tipo', params.tipo);
        if (params?.isActive !== undefined) searchParams.append('isActive', String(params.isActive));
        if (params?.search) searchParams.append('search', params.search);
        if (params?.revisioneProssimaGiorni !== undefined) searchParams.append('revisioneProssimaGiorni', String(params.revisioneProssimaGiorni));
        if (params?.page !== undefined) searchParams.append('page', String(params.page));
        if (params?.pageSize !== undefined) searchParams.append('pageSize', String(params.pageSize));

        return {
          url: `/api/v1/vehicles?${searchParams.toString()}`,
          method: 'GET',
        };
      },
      providesTags: (result) =>
        result
          ? [
              ...result.vehicles.map(({ id }) => ({ type: 'Vehicle' as const, id })),
              { type: 'Vehicle', id: 'LIST' },
            ]
          : [{ type: 'Vehicle', id: 'LIST' }],
    }),

    // Get vehicle by ID
    getVehicleById: builder.query<VehicleResponse, string>({
      query: (id) => `/api/v1/vehicles/${id}`,
      providesTags: (_result, _error, id) => [{ type: 'Vehicle', id }],
    }),

    // Get vehicles with expiring revisions
    getExpiringVehicles: builder.query<GetExpiringVehiclesResponse, { days?: number }>({
      query: ({ days = 30 }) => {
        const searchParams = new URLSearchParams();
        searchParams.append('days', String(days));

        return {
          url: `/api/v1/vehicles/expiring?${searchParams.toString()}`,
          method: 'GET',
        };
      },
      providesTags: [{ type: 'Vehicle', id: 'EXPIRING' }],
    }),

    // Create new vehicle
    createVehicle: builder.mutation<VehicleResponse, CreateVehicleInput>({
      query: (vehicleData) => ({
        url: '/api/v1/vehicles',
        method: 'POST',
        body: vehicleData,
      }),
      invalidatesTags: [{ type: 'Vehicle', id: 'LIST' }, { type: 'Vehicle', id: 'EXPIRING' }],
    }),

    // Update vehicle
    updateVehicle: builder.mutation<VehicleResponse, { id: string; data: UpdateVehicleInput }>({
      query: ({ id, data }) => ({
        url: `/api/v1/vehicles/${id}`,
        method: 'PUT',
        body: data,
      }),
      invalidatesTags: (_result, _error, { id }) => [
        { type: 'Vehicle', id },
        { type: 'Vehicle', id: 'LIST' },
        { type: 'Vehicle', id: 'EXPIRING' },
      ],
    }),

    // Delete vehicle
    deleteVehicle: builder.mutation<DeleteVehicleResponse, string>({
      query: (id) => ({
        url: `/api/v1/vehicles/${id}`,
        method: 'DELETE',
      }),
      invalidatesTags: (_result, _error, id) => [
        { type: 'Vehicle', id },
        { type: 'Vehicle', id: 'LIST' },
        { type: 'Vehicle', id: 'EXPIRING' },
      ],
    }),
  }),
});

// Export hooks for usage in components
export const {
  useGetVehiclesQuery,
  useGetVehicleByIdQuery,
  useGetExpiringVehiclesQuery,
  useCreateVehicleMutation,
  useUpdateVehicleMutation,
  useDeleteVehicleMutation,
} = vehicleApi;