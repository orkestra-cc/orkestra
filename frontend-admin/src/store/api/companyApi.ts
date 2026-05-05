import { baseApi } from './baseApi';
import type {
  CompanyLookup,
  CompanyLookupListResponse,
  CompanyLookupListParams,
  CompanyLookupSearchParams,
  EnrichCompanyParams,
  CompanySearchApiParams,
  CompanySearchResult,
} from '../../types/company';

// Helper to build query params
const buildQueryParams = (params: Record<string, unknown>): string => {
  const searchParams = new URLSearchParams();
  Object.entries(params).forEach(([key, value]) => {
    if (value !== undefined && value !== null && value !== '') {
      searchParams.append(key, String(value));
    }
  });
  return searchParams.toString();
};

// Company Lookup API endpoints
export const companyApi = baseApi.injectEndpoints({
  endpoints: (builder) => ({
    // Live lookup by tax code / P.IVA (external API call, cached)
    lookupCompany: builder.query<CompanyLookup, string>({
      query: (taxCode) => `/v1/company/lookup/${taxCode}`,
      async onQueryStarted(_taxCode, { dispatch, queryFulfilled }) {
        try {
          await queryFulfilled;
          // Invalidate the list so the table refetches with the new lookup
          dispatch(
            baseApi.util.invalidateTags([{ type: 'CompanyLookup', id: 'LIST' }])
          );
        } catch {
          // Lookup failed, no invalidation needed
        }
      },
    }),

    // List all stored lookups (paginated)
    getCompanyLookups: builder.query<CompanyLookupListResponse, CompanyLookupListParams | undefined>({
      query: (params) => {
        const queryString = params ? buildQueryParams(params) : '';
        return {
          url: `/v1/company/lookups${queryString ? `?${queryString}` : ''}`,
          method: 'GET',
        };
      },
      providesTags: (result) =>
        result?.lookups
          ? [
              ...result.lookups.map(({ uuid }) => ({ type: 'CompanyLookup' as const, id: uuid })),
              { type: 'CompanyLookup', id: 'LIST' },
            ]
          : [{ type: 'CompanyLookup', id: 'LIST' }],
    }),

    // Search stored lookups
    searchCompanyLookups: builder.query<CompanyLookupListResponse, CompanyLookupSearchParams>({
      query: (params) => {
        const queryString = buildQueryParams(params);
        return {
          url: `/v1/company/lookups/search${queryString ? `?${queryString}` : ''}`,
          method: 'GET',
        };
      },
      providesTags: (result) =>
        result?.lookups
          ? [
              ...result.lookups.map(({ uuid }) => ({ type: 'CompanyLookup' as const, id: uuid })),
              { type: 'CompanyLookup', id: 'LIST' },
            ]
          : [{ type: 'CompanyLookup', id: 'LIST' }],
    }),

    // Get specific lookup by UUID
    getCompanyLookup: builder.query<CompanyLookup, string>({
      query: (id) => `/v1/company/lookups/${id}`,
      providesTags: (_result, _error, id) => [{ type: 'CompanyLookup', id }],
    }),

    // Search companies via IT-search API
    searchCompanies: builder.query<CompanySearchResult, CompanySearchApiParams>({
      query: (params) => {
        const queryString = buildQueryParams(params as Record<string, unknown>);
        return `/v1/company/search${queryString ? `?${queryString}` : ''}`;
      },
      async onQueryStarted(_params, { dispatch, queryFulfilled }) {
        try {
          await queryFulfilled;
          dispatch(baseApi.util.invalidateTags([{ type: 'CompanyLookup', id: 'LIST' }]));
        } catch { /* noop */ }
      },
    }),

    // Enrich a company lookup with additional data
    enrichCompanyLookup: builder.query<CompanyLookup, EnrichCompanyParams>({
      query: ({ taxCode, type }) => `/v1/company/lookup/${taxCode}/enrich/${type}`,
      async onQueryStarted(_params, { dispatch, queryFulfilled }) {
        try {
          await queryFulfilled;
          dispatch(
            baseApi.util.invalidateTags([{ type: 'CompanyLookup', id: 'LIST' }])
          );
        } catch {
          // Enrichment failed, no invalidation needed
        }
      },
    }),
  }),
});

// Export hooks for usage in components
export const {
  useLazyLookupCompanyQuery,
  useGetCompanyLookupsQuery,
  useSearchCompanyLookupsQuery,
  useGetCompanyLookupQuery,
  useLazySearchCompaniesQuery,
  useLazyEnrichCompanyLookupQuery,
} = companyApi;
