// RTK Query slice for the marketing addon (Phase 1).
//
// Surface mirrors backend/internal/addons/marketing/handlers/ — 5
// resource families plus the import-job surface. Cache tag types are
// declared in baseApi.ts (MarketingOrg, MarketingPerson,
// MarketingMembership, MarketingTag, MarketingCustomFieldSchema,
// MarketingImport).
//
// All endpoints sit on the operator host (console.*) — backend
// route registration scopes them to RequireInternalTenant + the
// matching marketing.contact.* / marketing.import.run permission.

import { baseApi } from './baseApi';
import type {
  Organization,
  OrganizationPayload,
  Person,
  PersonPayload,
  Membership,
  MembershipPayload,
  Tag,
  TagPayload,
  CustomFieldSchema,
  CustomFieldSchemaPayload,
  CustomFieldTarget,
  ImportJob,
  PaginatedItems,
  SimpleItems
} from '../../types/marketing';

const buildQS = (params: Record<string, unknown>): string => {
  const sp = new URLSearchParams();
  Object.entries(params).forEach(([k, v]) => {
    if (v === undefined || v === null || v === '') return;
    if (Array.isArray(v)) {
      v.forEach(item => sp.append(k, String(item)));
      return;
    }
    sp.append(k, String(v));
  });
  const qs = sp.toString();
  return qs ? `?${qs}` : '';
};

export const marketingApi = baseApi.injectEndpoints({
  endpoints: builder => ({
    // --- Organizations -------------------------------------------------

    listMarketingOrgs: builder.query<
      PaginatedItems<Organization>,
      | {
          kind?: string;
          tag?: string[];
          source?: string;
          limit?: number;
          skip?: number;
        }
      | undefined
    >({
      query: params =>
        `/v1/marketing/organizations${params ? buildQS(params) : ''}`,
      providesTags: result =>
        result?.items
          ? [
              ...result.items.map(({ uuid }) => ({
                type: 'MarketingOrg' as const,
                id: uuid
              })),
              { type: 'MarketingOrg', id: 'LIST' }
            ]
          : [{ type: 'MarketingOrg', id: 'LIST' }]
    }),
    getMarketingOrg: builder.query<Organization, string>({
      query: id => `/v1/marketing/organizations/${id}`,
      transformResponse: (r: Organization) => r,
      providesTags: (_r, _e, id) => [{ type: 'MarketingOrg', id }]
    }),
    createMarketingOrg: builder.mutation<Organization, OrganizationPayload>({
      query: body => ({
        url: '/v1/marketing/organizations',
        method: 'POST',
        body
      }),
      invalidatesTags: [{ type: 'MarketingOrg', id: 'LIST' }]
    }),
    updateMarketingOrg: builder.mutation<
      Organization,
      { id: string; patch: Record<string, unknown> }
    >({
      query: ({ id, patch }) => ({
        url: `/v1/marketing/organizations/${id}`,
        method: 'PATCH',
        body: patch
      }),
      invalidatesTags: (_r, _e, { id }) => [
        { type: 'MarketingOrg', id },
        { type: 'MarketingOrg', id: 'LIST' }
      ]
    }),
    deleteMarketingOrg: builder.mutation<void, string>({
      query: id => ({
        url: `/v1/marketing/organizations/${id}`,
        method: 'DELETE'
      }),
      invalidatesTags: (_r, _e, id) => [
        { type: 'MarketingOrg', id },
        { type: 'MarketingOrg', id: 'LIST' },
        // Memberships cascade on org delete server-side.
        { type: 'MarketingMembership', id: 'LIST' }
      ]
    }),

    // --- Persons -------------------------------------------------------

    listMarketingPersons: builder.query<
      PaginatedItems<Person>,
      | {
          tag?: string[];
          hasEmail?: boolean;
          source?: string;
          limit?: number;
          skip?: number;
        }
      | undefined
    >({
      query: params => `/v1/marketing/persons${params ? buildQS(params) : ''}`,
      providesTags: result =>
        result?.items
          ? [
              ...result.items.map(({ uuid }) => ({
                type: 'MarketingPerson' as const,
                id: uuid
              })),
              { type: 'MarketingPerson', id: 'LIST' }
            ]
          : [{ type: 'MarketingPerson', id: 'LIST' }]
    }),
    getMarketingPerson: builder.query<Person, string>({
      query: id => `/v1/marketing/persons/${id}`,
      providesTags: (_r, _e, id) => [{ type: 'MarketingPerson', id }]
    }),
    createMarketingPerson: builder.mutation<Person, PersonPayload>({
      query: body => ({
        url: '/v1/marketing/persons',
        method: 'POST',
        body
      }),
      invalidatesTags: [{ type: 'MarketingPerson', id: 'LIST' }]
    }),
    updateMarketingPerson: builder.mutation<
      Person,
      { id: string; patch: Record<string, unknown> }
    >({
      query: ({ id, patch }) => ({
        url: `/v1/marketing/persons/${id}`,
        method: 'PATCH',
        body: patch
      }),
      invalidatesTags: (_r, _e, { id }) => [
        { type: 'MarketingPerson', id },
        { type: 'MarketingPerson', id: 'LIST' }
      ]
    }),
    deleteMarketingPerson: builder.mutation<void, string>({
      query: id => ({
        url: `/v1/marketing/persons/${id}`,
        method: 'DELETE'
      }),
      invalidatesTags: (_r, _e, id) => [
        { type: 'MarketingPerson', id },
        { type: 'MarketingPerson', id: 'LIST' },
        { type: 'MarketingMembership', id: 'LIST' }
      ]
    }),

    // --- Memberships (Person sub-resource) ----------------------------

    listPersonMemberships: builder.query<SimpleItems<Membership>, string>({
      query: personId => `/v1/marketing/persons/${personId}/memberships`,
      providesTags: (result, _e, personId) =>
        result?.items
          ? [
              ...result.items.map(({ uuid }) => ({
                type: 'MarketingMembership' as const,
                id: uuid
              })),
              { type: 'MarketingMembership', id: `person:${personId}` }
            ]
          : [{ type: 'MarketingMembership', id: `person:${personId}` }]
    }),
    createPersonMembership: builder.mutation<
      Membership,
      { personId: string; body: MembershipPayload }
    >({
      query: ({ personId, body }) => ({
        url: `/v1/marketing/persons/${personId}/memberships`,
        method: 'POST',
        body
      }),
      invalidatesTags: (_r, _e, { personId }) => [
        { type: 'MarketingMembership', id: 'LIST' },
        { type: 'MarketingMembership', id: `person:${personId}` },
        { type: 'MarketingPerson', id: personId }
      ]
    }),
    updateMembership: builder.mutation<
      Membership,
      { id: string; patch: Record<string, unknown> }
    >({
      query: ({ id, patch }) => ({
        url: `/v1/marketing/memberships/${id}`,
        method: 'PATCH',
        body: patch
      }),
      invalidatesTags: (_r, _e, { id }) => [
        { type: 'MarketingMembership', id },
        { type: 'MarketingMembership', id: 'LIST' }
      ]
    }),
    deleteMembership: builder.mutation<void, string>({
      query: id => ({
        url: `/v1/marketing/memberships/${id}`,
        method: 'DELETE'
      }),
      invalidatesTags: (_r, _e, id) => [
        { type: 'MarketingMembership', id },
        { type: 'MarketingMembership', id: 'LIST' }
      ]
    }),

    // --- Tags ----------------------------------------------------------

    listMarketingTags: builder.query<SimpleItems<Tag>, void>({
      query: () => '/v1/marketing/tags',
      providesTags: result =>
        result?.items
          ? [
              ...result.items.map(({ uuid }) => ({
                type: 'MarketingTag' as const,
                id: uuid
              })),
              { type: 'MarketingTag', id: 'LIST' }
            ]
          : [{ type: 'MarketingTag', id: 'LIST' }]
    }),
    createMarketingTag: builder.mutation<Tag, TagPayload>({
      query: body => ({ url: '/v1/marketing/tags', method: 'POST', body }),
      invalidatesTags: [{ type: 'MarketingTag', id: 'LIST' }]
    }),
    updateMarketingTag: builder.mutation<
      Tag,
      { id: string; patch: Record<string, unknown> }
    >({
      query: ({ id, patch }) => ({
        url: `/v1/marketing/tags/${id}`,
        method: 'PATCH',
        body: patch
      }),
      invalidatesTags: (_r, _e, { id }) => [
        { type: 'MarketingTag', id },
        { type: 'MarketingTag', id: 'LIST' }
      ]
    }),
    deleteMarketingTag: builder.mutation<void, string>({
      query: id => ({
        url: `/v1/marketing/tags/${id}`,
        method: 'DELETE'
      }),
      invalidatesTags: (_r, _e, id) => [
        { type: 'MarketingTag', id },
        { type: 'MarketingTag', id: 'LIST' }
      ]
    }),

    // --- Custom Field Schemas -----------------------------------------

    listCustomFieldSchemas: builder.query<SimpleItems<CustomFieldSchema>, void>(
      {
        query: () => '/v1/marketing/custom-field-schemas',
        providesTags: result =>
          result?.items
            ? [
                ...result.items.map(({ uuid }) => ({
                  type: 'MarketingCustomFieldSchema' as const,
                  id: uuid
                })),
                { type: 'MarketingCustomFieldSchema', id: 'LIST' }
              ]
            : [{ type: 'MarketingCustomFieldSchema', id: 'LIST' }]
      }
    ),
    getCustomFieldSchema: builder.query<CustomFieldSchema, CustomFieldTarget>({
      query: target => `/v1/marketing/custom-field-schemas/${target}`,
      providesTags: (_r, _e, target) => [
        { type: 'MarketingCustomFieldSchema', id: target }
      ]
    }),
    upsertCustomFieldSchema: builder.mutation<
      CustomFieldSchema,
      CustomFieldSchemaPayload
    >({
      query: body => ({
        url: '/v1/marketing/custom-field-schemas',
        method: 'PUT',
        body
      }),
      invalidatesTags: (_r, _e, body) => [
        { type: 'MarketingCustomFieldSchema', id: 'LIST' },
        { type: 'MarketingCustomFieldSchema', id: body.targetCollection }
      ]
    }),
    deleteCustomFieldSchema: builder.mutation<void, CustomFieldTarget>({
      query: target => ({
        url: `/v1/marketing/custom-field-schemas/${target}`,
        method: 'DELETE'
      }),
      invalidatesTags: (_r, _e, target) => [
        { type: 'MarketingCustomFieldSchema', id: 'LIST' },
        { type: 'MarketingCustomFieldSchema', id: target }
      ]
    }),

    // --- Imports -------------------------------------------------------

    listMarketingImports: builder.query<
      PaginatedItems<ImportJob>,
      { limit?: number; skip?: number } | undefined
    >({
      query: params => `/v1/marketing/imports${params ? buildQS(params) : ''}`,
      providesTags: result =>
        result?.items
          ? [
              ...result.items.map(({ uuid }) => ({
                type: 'MarketingImport' as const,
                id: uuid
              })),
              { type: 'MarketingImport', id: 'LIST' }
            ]
          : [{ type: 'MarketingImport', id: 'LIST' }]
    }),
    getMarketingImport: builder.query<ImportJob, string>({
      query: id => `/v1/marketing/imports/${id}`,
      providesTags: (_r, _e, id) => [{ type: 'MarketingImport', id }]
    })
    // runMarketingImport is intentionally NOT an RTK Query mutation —
    // multipart/form-data uploads don't fit cleanly through the
    // generated fetchBaseQuery serializer. The import wizard calls
    // `fetch` directly with credentials:'include' and then
    // invalidates the 'MarketingImport' / 'MarketingOrg' /
    // 'MarketingPerson' tags via `useDispatch + invalidateApiTags`.
  })
});

export const {
  useListMarketingOrgsQuery,
  useGetMarketingOrgQuery,
  useCreateMarketingOrgMutation,
  useUpdateMarketingOrgMutation,
  useDeleteMarketingOrgMutation,
  useListMarketingPersonsQuery,
  useGetMarketingPersonQuery,
  useCreateMarketingPersonMutation,
  useUpdateMarketingPersonMutation,
  useDeleteMarketingPersonMutation,
  useListPersonMembershipsQuery,
  useCreatePersonMembershipMutation,
  useUpdateMembershipMutation,
  useDeleteMembershipMutation,
  useListMarketingTagsQuery,
  useCreateMarketingTagMutation,
  useUpdateMarketingTagMutation,
  useDeleteMarketingTagMutation,
  useListCustomFieldSchemasQuery,
  useGetCustomFieldSchemaQuery,
  useUpsertCustomFieldSchemaMutation,
  useDeleteCustomFieldSchemaMutation,
  useListMarketingImportsQuery,
  useGetMarketingImportQuery
} = marketingApi;
