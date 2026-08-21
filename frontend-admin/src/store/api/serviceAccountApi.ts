import { baseApi } from 'store/api/baseApi';
import type {
  CredentialWithSecret,
  ServiceAccount,
  ServiceAccountDetail,
  ServiceAccountWithSecret
} from 'types/serviceAccounts';

export const serviceAccountApi = baseApi.injectEndpoints({
  endpoints: build => ({
    listServiceAccounts: build.query<ServiceAccount[], void>({
      query: () => '/v1/admin/service-accounts',
      providesTags: result => [
        { type: 'ServiceAccount' as const, id: 'LIST' },
        ...(result ?? []).map(a => ({
          type: 'ServiceAccount' as const,
          id: a.id
        }))
      ]
    }),
    getServiceAccount: build.query<ServiceAccountDetail, string>({
      query: id => `/v1/admin/service-accounts/${encodeURIComponent(id)}`,
      providesTags: (_r, _e, id) => [{ type: 'ServiceAccount', id }]
    }),
    createServiceAccount: build.mutation<
      ServiceAccountWithSecret,
      { name: string }
    >({
      query: body => ({
        url: '/v1/admin/service-accounts',
        method: 'POST',
        body
      }),
      invalidatesTags: [{ type: 'ServiceAccount', id: 'LIST' }]
    }),
    updateServiceAccount: build.mutation<
      ServiceAccount,
      { id: string; name?: string; active?: boolean }
    >({
      query: ({ id, ...body }) => ({
        url: `/v1/admin/service-accounts/${encodeURIComponent(id)}`,
        method: 'PATCH',
        body
      }),
      invalidatesTags: (_r, _e, { id }) => [
        { type: 'ServiceAccount', id },
        { type: 'ServiceAccount', id: 'LIST' }
      ]
    }),
    issueCredential: build.mutation<
      CredentialWithSecret,
      { id: string; label?: string }
    >({
      query: ({ id, ...body }) => ({
        url: `/v1/admin/service-accounts/${encodeURIComponent(id)}/credentials`,
        method: 'POST',
        body
      }),
      invalidatesTags: (_r, _e, { id }) => [
        { type: 'ServiceAccount', id },
        { type: 'ServiceAccount', id: 'LIST' }
      ]
    }),
    revokeCredential: build.mutation<
      void,
      { id: string; credentialId: string }
    >({
      query: ({ id, credentialId }) => ({
        url: `/v1/admin/service-accounts/${encodeURIComponent(id)}/credentials/${encodeURIComponent(credentialId)}`,
        method: 'DELETE'
      }),
      invalidatesTags: (_r, _e, { id }) => [
        { type: 'ServiceAccount', id },
        { type: 'ServiceAccount', id: 'LIST' }
      ]
    })
  })
});

export const {
  useListServiceAccountsQuery,
  useGetServiceAccountQuery,
  useCreateServiceAccountMutation,
  useUpdateServiceAccountMutation,
  useIssueCredentialMutation,
  useRevokeCredentialMutation
} = serviceAccountApi;
