import { baseApi } from './baseApi';
import type {
  Customer,
  CustomerListResponse,
  CustomerListParams,
  CreateCustomerInput,
  UpdateCustomerInput,
  Supplier,
  SupplierListResponse,
  SupplierListParams,
  CreateSupplierInput,
  UpdateSupplierInput,
  Company,
  CompanyListResponse,
  CompanyListParams,
  CreateCompanyInput,
  UpdateCompanyInput,
  Invoice,
  InvoiceListResponse,
  InvoiceListParams,
  CreateInvoiceInput,
  UpdateInvoiceInput,
  SendInvoiceResponse,
  SDINotification,
  NotificationListResponse,
  NotificationListParams,
  NotificationSummary,
  BillingStats,
  BillingStatsParams,
  PreservedDocument,
  ImportInvoiceInput,
  ImportInvoiceResponse,
} from '../../types/billing';

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

// Billing API endpoints
export const billingApi = baseApi.injectEndpoints({
  endpoints: (builder) => ({
    // ========================================
    // Customer Endpoints
    // ========================================

    getCustomers: builder.query<CustomerListResponse, CustomerListParams | undefined>({
      query: (params) => {
        const queryString = params ? buildQueryParams(params) : '';
        return {
          url: `/api/v1/billing/customers${queryString ? `?${queryString}` : ''}`,
          method: 'GET',
        };
      },
      providesTags: (result) =>
        result?.customers
          ? [
              ...result.customers.map(({ id }) => ({ type: 'Customer' as const, id })),
              { type: 'Customer', id: 'LIST' },
            ]
          : [{ type: 'Customer', id: 'LIST' }],
    }),

    getCustomer: builder.query<Customer, string>({
      query: (id) => `/api/v1/billing/customers/${id}`,
      providesTags: (_result, _error, id) => [{ type: 'Customer', id }],
    }),

    createCustomer: builder.mutation<Customer, CreateCustomerInput>({
      query: (data) => ({
        url: '/api/v1/billing/customers',
        method: 'POST',
        body: data,
      }),
      invalidatesTags: [{ type: 'Customer', id: 'LIST' }],
    }),

    updateCustomer: builder.mutation<Customer, { id: string; data: UpdateCustomerInput }>({
      query: ({ id, data }) => ({
        url: `/api/v1/billing/customers/${id}`,
        method: 'PATCH',
        body: data,
      }),
      invalidatesTags: (_result, _error, { id }) => [
        { type: 'Customer', id },
        { type: 'Customer', id: 'LIST' },
      ],
    }),

    deleteCustomer: builder.mutation<void, string>({
      query: (id) => ({
        url: `/api/v1/billing/customers/${id}`,
        method: 'DELETE',
      }),
      invalidatesTags: (_result, _error, id) => [
        { type: 'Customer', id },
        { type: 'Customer', id: 'LIST' },
      ],
    }),

    // ========================================
    // Supplier Endpoints
    // ========================================

    getSuppliers: builder.query<SupplierListResponse, SupplierListParams | undefined>({
      query: (params) => {
        const queryString = params ? buildQueryParams(params) : '';
        return {
          url: `/api/v1/billing/suppliers${queryString ? `?${queryString}` : ''}`,
          method: 'GET',
        };
      },
      providesTags: (result) =>
        result?.suppliers
          ? [
              ...result.suppliers.map(({ id }) => ({ type: 'Supplier' as const, id })),
              { type: 'Supplier', id: 'LIST' },
            ]
          : [{ type: 'Supplier', id: 'LIST' }],
    }),

    getSupplier: builder.query<Supplier, string>({
      query: (id) => `/api/v1/billing/suppliers/${id}`,
      providesTags: (_result, _error, id) => [{ type: 'Supplier', id }],
    }),

    createSupplier: builder.mutation<Supplier, CreateSupplierInput>({
      query: (data) => ({
        url: '/api/v1/billing/suppliers',
        method: 'POST',
        body: data,
      }),
      invalidatesTags: [{ type: 'Supplier', id: 'LIST' }],
    }),

    updateSupplier: builder.mutation<Supplier, { id: string; data: UpdateSupplierInput }>({
      query: ({ id, data }) => ({
        url: `/api/v1/billing/suppliers/${id}`,
        method: 'PATCH',
        body: data,
      }),
      invalidatesTags: (_result, _error, { id }) => [
        { type: 'Supplier', id },
        { type: 'Supplier', id: 'LIST' },
      ],
    }),

    deleteSupplier: builder.mutation<void, string>({
      query: (id) => ({
        url: `/api/v1/billing/suppliers/${id}`,
        method: 'DELETE',
      }),
      invalidatesTags: (_result, _error, id) => [
        { type: 'Supplier', id },
        { type: 'Supplier', id: 'LIST' },
      ],
    }),

    // ========================================
    // Company Endpoints (Issuing Companies)
    // ========================================

    getCompanies: builder.query<CompanyListResponse, CompanyListParams | undefined>({
      query: (params) => {
        const queryString = params ? buildQueryParams(params) : '';
        return {
          url: `/api/v1/billing/companies${queryString ? `?${queryString}` : ''}`,
          method: 'GET',
        };
      },
      providesTags: (result) =>
        result?.companies
          ? [
              ...result.companies.map(({ id }) => ({ type: 'Company' as const, id })),
              { type: 'Company', id: 'LIST' },
            ]
          : [{ type: 'Company', id: 'LIST' }],
    }),

    getCompany: builder.query<Company, string>({
      query: (id) => `/api/v1/billing/companies/${id}`,
      providesTags: (_result, _error, id) => [{ type: 'Company', id }],
    }),

    getDefaultCompany: builder.query<Company, void>({
      query: () => '/api/v1/billing/companies/default',
      providesTags: [{ type: 'Company', id: 'DEFAULT' }],
    }),

    createCompany: builder.mutation<Company, CreateCompanyInput>({
      query: (data) => ({
        url: '/api/v1/billing/companies',
        method: 'POST',
        body: data,
      }),
      invalidatesTags: [
        { type: 'Company', id: 'LIST' },
        { type: 'Company', id: 'DEFAULT' },
      ],
    }),

    updateCompany: builder.mutation<Company, { id: string; data: UpdateCompanyInput }>({
      query: ({ id, data }) => ({
        url: `/api/v1/billing/companies/${id}`,
        method: 'PATCH',
        body: data,
      }),
      invalidatesTags: (_result, _error, { id }) => [
        { type: 'Company', id },
        { type: 'Company', id: 'LIST' },
        { type: 'Company', id: 'DEFAULT' },
      ],
    }),

    deleteCompany: builder.mutation<void, string>({
      query: (id) => ({
        url: `/api/v1/billing/companies/${id}`,
        method: 'DELETE',
      }),
      invalidatesTags: (_result, _error, id) => [
        { type: 'Company', id },
        { type: 'Company', id: 'LIST' },
        { type: 'Company', id: 'DEFAULT' },
      ],
    }),

    setDefaultCompany: builder.mutation<Company, string>({
      query: (id) => ({
        url: `/api/v1/billing/companies/${id}/default`,
        method: 'POST',
      }),
      invalidatesTags: [
        { type: 'Company', id: 'LIST' },
        { type: 'Company', id: 'DEFAULT' },
      ],
    }),

    // ========================================
    // Invoice Endpoints (Issued - Fatture Attive)
    // ========================================

    getInvoices: builder.query<InvoiceListResponse, InvoiceListParams | undefined>({
      query: (params) => {
        const queryString = params ? buildQueryParams(params) : '';
        return {
          url: `/api/v1/billing/invoices${queryString ? `?${queryString}` : ''}`,
          method: 'GET',
        };
      },
      providesTags: (result) =>
        result?.invoices
          ? [
              ...result.invoices.map(({ id }) => ({ type: 'Invoice' as const, id })),
              { type: 'Invoice', id: 'LIST' },
            ]
          : [{ type: 'Invoice', id: 'LIST' }],
    }),

    getInvoice: builder.query<Invoice, string>({
      query: (id) => `/api/v1/billing/invoices/${id}`,
      transformResponse: (response: { invoice: Invoice } | Invoice) => {
        // Handle both wrapped and unwrapped response formats
        const invoice = 'invoice' in response ? response.invoice : response;
        // Ensure arrays are never null
        if (invoice) {
          invoice.lines = invoice.lines || [];
          invoice.causale = invoice.causale || [];
          invoice.datiRitenuta = invoice.datiRitenuta || [];
          invoice.datiCassaPrevidenziale = invoice.datiCassaPrevidenziale || [];
        }
        return invoice;
      },
      providesTags: (_result, _error, id) => [{ type: 'Invoice', id }],
    }),

    createInvoice: builder.mutation<Invoice, CreateInvoiceInput>({
      query: (data) => ({
        url: '/api/v1/billing/invoices',
        method: 'POST',
        body: data,
      }),
      invalidatesTags: [
        { type: 'Invoice', id: 'LIST' },
        { type: 'BillingStats', id: 'STATS' },
      ],
    }),

    updateInvoice: builder.mutation<Invoice, { id: string; data: UpdateInvoiceInput }>({
      query: ({ id, data }) => ({
        url: `/api/v1/billing/invoices/${id}`,
        method: 'PATCH',
        body: data,
      }),
      invalidatesTags: (_result, _error, { id }) => [
        { type: 'Invoice', id },
        { type: 'Invoice', id: 'LIST' },
      ],
    }),

    deleteInvoice: builder.mutation<void, string>({
      query: (id) => ({
        url: `/api/v1/billing/invoices/${id}`,
        method: 'DELETE',
      }),
      invalidatesTags: (_result, _error, id) => [
        { type: 'Invoice', id },
        { type: 'Invoice', id: 'LIST' },
        { type: 'BillingStats', id: 'STATS' },
      ],
    }),

    sendInvoice: builder.mutation<SendInvoiceResponse, string>({
      query: (id) => ({
        url: `/api/v1/billing/invoices/${id}/send`,
        method: 'POST',
      }),
      invalidatesTags: (_result, _error, id) => [
        { type: 'Invoice', id },
        { type: 'Invoice', id: 'LIST' },
        { type: 'BillingStats', id: 'STATS' },
      ],
    }),

    getInvoiceXml: builder.query<string, string>({
      query: (id) => ({
        url: `/api/v1/billing/invoices/${id}/xml`,
        method: 'GET',
      }),
      transformResponse: (response: { xml: string }) => response.xml,
    }),

    getInvoiceHtml: builder.query<string, string>({
      query: (id) => ({
        url: `/api/v1/billing/invoices/${id}/html`,
        method: 'GET',
      }),
      transformResponse: (response: { html: string }) => response.html,
    }),

    // PDF download - uses raw fetch to bypass RTK Query serialization
    getInvoicePdf: builder.query<{ success: true }, { id: string; filename?: string }>({
      queryFn: async ({ id, filename }, { getState }) => {
        try {
          const baseUrl = import.meta.env.VITE_BACKEND_URL || 'http://localhost:3000';
          const state = getState() as { auth?: { accessToken?: string; tokenExpiry?: string } };

          const headers: HeadersInit = {};
          if (state.auth?.accessToken && state.auth?.tokenExpiry && new Date(state.auth.tokenExpiry) > new Date()) {
            headers['Authorization'] = `Bearer ${state.auth.accessToken}`;
          }

          // Use raw fetch to completely bypass RTK Query's action dispatch
          const response = await fetch(`${baseUrl}/api/v1/billing/invoices/${id}/download`, {
            method: 'GET',
            credentials: 'include',
            headers,
          });

          if (!response.ok) {
            return { error: { status: response.status, data: 'Failed to download PDF' } };
          }

          // Get blob and trigger download directly
          const blob = await response.blob();
          const url = window.URL.createObjectURL(blob);
          const link = document.createElement('a');
          link.href = url;
          link.download = filename || `fattura_${id}.pdf`;
          document.body.appendChild(link);
          link.click();
          document.body.removeChild(link);
          window.URL.revokeObjectURL(url);

          // Return simple success indicator (serializable)
          return { data: { success: true as const } };
        } catch (error) {
          return { error: { status: 'FETCH_ERROR', error: String(error) } };
        }
      },
      // Don't cache this - each download is a fresh fetch
      keepUnusedDataFor: 0,
    }),

    importInvoice: builder.mutation<ImportInvoiceResponse, ImportInvoiceInput>({
      query: (data) => ({
        url: '/api/v1/billing/invoices/import',
        method: 'POST',
        body: data,
      }),
      invalidatesTags: [
        { type: 'Invoice', id: 'LIST' },
        { type: 'BillingStats', id: 'STATS' },
      ],
    }),

    // ========================================
    // Received Invoice Endpoints (Fatture Passive)
    // ========================================

    getReceivedInvoices: builder.query<InvoiceListResponse, InvoiceListParams | undefined>({
      query: (params) => {
        const queryString = params ? buildQueryParams({ ...params, direction: 'received' }) : 'direction=received';
        return {
          url: `/api/v1/billing/received-invoices${queryString ? `?${queryString}` : ''}`,
          method: 'GET',
        };
      },
      providesTags: (result) =>
        result?.invoices
          ? [
              ...result.invoices.map(({ id }) => ({ type: 'Invoice' as const, id })),
              { type: 'Invoice', id: 'RECEIVED_LIST' },
            ]
          : [{ type: 'Invoice', id: 'RECEIVED_LIST' }],
    }),

    getReceivedInvoice: builder.query<Invoice, string>({
      query: (id) => `/api/v1/billing/received-invoices/${id}`,
      providesTags: (_result, _error, id) => [{ type: 'Invoice', id }],
    }),

    acceptInvoice: builder.mutation<Invoice, string>({
      query: (id) => ({
        url: `/api/v1/billing/received-invoices/${id}/accept`,
        method: 'POST',
      }),
      invalidatesTags: (_result, _error, id) => [
        { type: 'Invoice', id },
        { type: 'Invoice', id: 'RECEIVED_LIST' },
        { type: 'BillingStats', id: 'STATS' },
      ],
    }),

    rejectInvoice: builder.mutation<Invoice, { id: string; reason: string }>({
      query: ({ id, reason }) => ({
        url: `/api/v1/billing/received-invoices/${id}/reject`,
        method: 'POST',
        body: { reason },
      }),
      invalidatesTags: (_result, _error, { id }) => [
        { type: 'Invoice', id },
        { type: 'Invoice', id: 'RECEIVED_LIST' },
        { type: 'BillingStats', id: 'STATS' },
      ],
    }),

    // ========================================
    // Notification Endpoints
    // ========================================

    getNotifications: builder.query<NotificationListResponse, NotificationListParams | undefined>({
      query: (params) => {
        const queryString = params ? buildQueryParams(params) : '';
        return {
          url: `/api/v1/billing/notifications${queryString ? `?${queryString}` : ''}`,
          method: 'GET',
        };
      },
      providesTags: (result) =>
        result?.notifications
          ? [
              ...result.notifications.map(({ id }) => ({ type: 'Notification' as const, id })),
              { type: 'Notification', id: 'LIST' },
            ]
          : [{ type: 'Notification', id: 'LIST' }],
    }),

    getNotification: builder.query<SDINotification, string>({
      query: (id) => `/api/v1/billing/notifications/${id}`,
      providesTags: (_result, _error, id) => [{ type: 'Notification', id }],
    }),

    markNotificationProcessed: builder.mutation<SDINotification, string>({
      query: (id) => ({
        url: `/api/v1/billing/notifications/${id}/process`,
        method: 'POST',
      }),
      invalidatesTags: (_result, _error, id) => [
        { type: 'Notification', id },
        { type: 'Notification', id: 'LIST' },
        { type: 'Notification', id: 'SUMMARY' },
        { type: 'BillingStats', id: 'STATS' },
      ],
    }),

    getNotificationSummary: builder.query<NotificationSummary, void>({
      query: () => '/api/v1/billing/notifications/summary',
      providesTags: [{ type: 'Notification', id: 'SUMMARY' }],
    }),

    // ========================================
    // Statistics Endpoint
    // ========================================

    getBillingStats: builder.query<BillingStats, BillingStatsParams | undefined>({
      query: (params) => {
        const queryString = params ? buildQueryParams(params) : '';
        return {
          url: `/api/v1/billing/stats${queryString ? `?${queryString}` : ''}`,
          method: 'GET',
        };
      },
      providesTags: [{ type: 'BillingStats', id: 'STATS' }],
    }),

    // ========================================
    // Preserved Documents Endpoint
    // ========================================

    getPreservedDocument: builder.query<PreservedDocument, string>({
      query: (id) => `/api/v1/billing/preserved-documents/${id}`,
    }),

    // ========================================
    // Business Registry Configuration Endpoints
    // ========================================

    getBusinessRegistryConfig: builder.query<BusinessRegistryConfig, string>({
      query: (fiscalId) => `/api/v1/billing/business-registry/${fiscalId}`,
      providesTags: (_result, _error, fiscalId) => [{ type: 'BusinessRegistry', id: fiscalId }],
    }),

    configureBusinessRegistry: builder.mutation<ConfigureBusinessRegistryResponse, ConfigureBusinessRegistryInput>({
      query: (data) => ({
        url: '/api/v1/billing/business-registry',
        method: 'POST',
        body: data,
      }),
      invalidatesTags: (_result, _error, { fiscalId }) => [{ type: 'BusinessRegistry', id: fiscalId }],
    }),
  }),
});

// Business Registry types
export interface BusinessRegistryConfig {
  fiscalId: string;
  email: string;
  applySignature: boolean;
  applyLegalStorage: boolean;
  active: boolean;
}

export interface ConfigureBusinessRegistryInput {
  fiscalId: string;
  email: string;
  applySignature: boolean;
  applyLegalStorage: boolean;
}

export interface ConfigureBusinessRegistryResponse {
  success: boolean;
  message: string;
}

// Export hooks for usage in components
export const {
  // Customer hooks
  useGetCustomersQuery,
  useGetCustomerQuery,
  useCreateCustomerMutation,
  useUpdateCustomerMutation,
  useDeleteCustomerMutation,
  // Supplier hooks
  useGetSuppliersQuery,
  useGetSupplierQuery,
  useCreateSupplierMutation,
  useUpdateSupplierMutation,
  useDeleteSupplierMutation,
  // Company hooks
  useGetCompaniesQuery,
  useGetCompanyQuery,
  useGetDefaultCompanyQuery,
  useCreateCompanyMutation,
  useUpdateCompanyMutation,
  useDeleteCompanyMutation,
  useSetDefaultCompanyMutation,
  // Invoice hooks
  useGetInvoicesQuery,
  useGetInvoiceQuery,
  useCreateInvoiceMutation,
  useUpdateInvoiceMutation,
  useDeleteInvoiceMutation,
  useSendInvoiceMutation,
  useGetInvoiceXmlQuery,
  useLazyGetInvoiceXmlQuery,
  useGetInvoiceHtmlQuery,
  useLazyGetInvoiceHtmlQuery,
  useLazyGetInvoicePdfQuery,
  useImportInvoiceMutation,
  // Received Invoice hooks
  useGetReceivedInvoicesQuery,
  useGetReceivedInvoiceQuery,
  useAcceptInvoiceMutation,
  useRejectInvoiceMutation,
  // Notification hooks
  useGetNotificationsQuery,
  useGetNotificationQuery,
  useMarkNotificationProcessedMutation,
  useGetNotificationSummaryQuery,
  // Statistics hooks
  useGetBillingStatsQuery,
  // Preserved Documents hooks
  useGetPreservedDocumentQuery,
  // Business Registry hooks
  useGetBusinessRegistryConfigQuery,
  useLazyGetBusinessRegistryConfigQuery,
  useConfigureBusinessRegistryMutation,
} = billingApi;
