import { baseApi } from './baseApi';

// Deadline report types based on backend OpenAPI
export type EntityType = 'user' | 'medical';
export type DeadlineStatus = 'expired' | 'warning' | 'ok';
export type DeadlineType =
  | 'license'
  | 'driver_card'
  | 'cqc'
  | 'adr'
  | 'medical_check';

export interface DeadlineItem {
  id: string;
  entityType: EntityType;
  entityId: string;
  entityName: string;
  deadlineType: DeadlineType;
  expiryDate: string;
  daysUntilExpiry: number;
  status: DeadlineStatus;
  notes?: string;
  doctor?: string;
  where?: string;
}

export interface DeadlineReportResponse {
  deadlines: DeadlineItem[];
  total: number;
  page: number;
  pageSize: number;
  totalPages: number;
}

export interface DeadlineReportParams {
  entityType?: EntityType;
  status?: DeadlineStatus;
  search?: string;
  page?: number;
  pageSize?: number;
}

// Reports API slice
export const reportsApi = baseApi.injectEndpoints({
  endpoints: (builder) => ({
    // Get deadline report with filtering and pagination
    getDeadlineReport: builder.query<DeadlineReportResponse, DeadlineReportParams | undefined>({
      query: (params) => {
        const searchParams = new URLSearchParams();

        // Add parameters if they exist
        if (params?.entityType) searchParams.append('entityType', params.entityType);
        if (params?.status) searchParams.append('status', params.status);
        if (params?.search) searchParams.append('search', params.search);
        if (params?.page !== undefined) searchParams.append('page', String(params.page));
        if (params?.pageSize !== undefined) searchParams.append('pageSize', String(params.pageSize));

        return {
          url: `/api/v1/reports/deadlines?${searchParams.toString()}`,
          method: 'GET',
        };
      },
      providesTags: [{ type: 'Reports', id: 'DEADLINES' }],
    }),
  }),
  overrideExisting: false,
});

// Export hooks for usage in components
export const {
  useGetDeadlineReportQuery,
} = reportsApi;
