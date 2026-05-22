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
  SimpleItems,
  Activity,
  ManualActivityPayload,
  ActivityKind,
  ActivitySource,
  ScoreProfile,
  ScoreProfilePayload,
  ScoreSnapshot,
  LeaderboardEntry,
  ConflictReview,
  ConflictReviewStatus,
  ConflictTargetKind,
  ResolveConflictPayload,
  DismissConflictPayload
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
    }),
    // runMarketingImport is intentionally NOT an RTK Query mutation —
    // multipart/form-data uploads don't fit cleanly through the
    // generated fetchBaseQuery serializer. The import wizard calls
    // `fetch` directly with credentials:'include' and then
    // invalidates the 'MarketingImport' / 'MarketingOrg' /
    // 'MarketingPerson' tags via `useDispatch + invalidateApiTags`.

    // --- Phase 2: Activities --------------------------------------------

    listPersonActivities: builder.query<
      PaginatedItems<Activity>,
      {
        personId: string;
        kind?: ActivityKind[];
        source?: ActivitySource;
        since?: string;
        until?: string;
        limit?: number;
        skip?: number;
      }
    >({
      query: ({ personId, ...rest }) =>
        `/v1/marketing/persons/${personId}/activities${buildQS(rest)}`,
      providesTags: (result, _e, { personId }) =>
        result?.items
          ? [
              ...result.items.map(({ uuid }) => ({
                type: 'MarketingActivity' as const,
                id: uuid
              })),
              { type: 'MarketingActivity', id: `person:${personId}` }
            ]
          : [{ type: 'MarketingActivity', id: `person:${personId}` }]
    }),
    createActivity: builder.mutation<Activity, ManualActivityPayload>({
      query: body => ({
        url: '/v1/marketing/activities',
        method: 'POST',
        body
      }),
      // A new activity invalidates the timeline for the affected
      // person AND every score snapshot of that person (eager
      // recompute on the backend produces fresh snapshots; the UI
      // re-fetches to reflect them).
      invalidatesTags: (_r, _e, body) => [
        { type: 'MarketingActivity', id: `person:${body.personUuid}` },
        { type: 'MarketingScoreSnapshot', id: `person:${body.personUuid}` }
      ]
    }),
    correctActivity: builder.mutation<
      Activity,
      { id: string; reason: string; personUuid: string }
    >({
      query: ({ id, reason }) => ({
        url: `/v1/marketing/activities/${id}/correct`,
        method: 'POST',
        body: { reason }
      }),
      invalidatesTags: (_r, _e, { personUuid }) => [
        { type: 'MarketingActivity', id: `person:${personUuid}` },
        { type: 'MarketingScoreSnapshot', id: `person:${personUuid}` }
      ]
    }),

    // --- Phase 2: Score Profiles ----------------------------------------

    listScoreProfiles: builder.query<
      SimpleItems<ScoreProfile>,
      { activeOnly?: boolean } | undefined
    >({
      query: params =>
        `/v1/marketing/score-profiles${params ? buildQS(params) : ''}`,
      providesTags: result =>
        result?.items
          ? [
              ...result.items.map(({ uuid }) => ({
                type: 'MarketingScoreProfile' as const,
                id: uuid
              })),
              { type: 'MarketingScoreProfile', id: 'LIST' }
            ]
          : [{ type: 'MarketingScoreProfile', id: 'LIST' }]
    }),
    getScoreProfile: builder.query<ScoreProfile, string>({
      query: id => `/v1/marketing/score-profiles/${id}`,
      providesTags: (_r, _e, id) => [{ type: 'MarketingScoreProfile', id }]
    }),
    createScoreProfile: builder.mutation<ScoreProfile, ScoreProfilePayload>({
      query: body => ({
        url: '/v1/marketing/score-profiles',
        method: 'POST',
        body
      }),
      invalidatesTags: [{ type: 'MarketingScoreProfile', id: 'LIST' }]
    }),
    replaceScoreProfile: builder.mutation<
      ScoreProfile,
      { id: string; body: ScoreProfilePayload }
    >({
      query: ({ id, body }) => ({
        url: `/v1/marketing/score-profiles/${id}`,
        method: 'PATCH',
        body
      }),
      // Save bumps the profile version and bulk-marks every downstream
      // snapshot stale. Invalidate the per-profile leaderboard cache
      // so the UI re-fetches the recomputed rows.
      invalidatesTags: (_r, _e, { id }) => [
        { type: 'MarketingScoreProfile', id },
        { type: 'MarketingScoreProfile', id: 'LIST' },
        { type: 'MarketingScoreSnapshot', id: `profile:${id}` }
      ]
    }),
    deleteScoreProfile: builder.mutation<void, string>({
      query: id => ({
        url: `/v1/marketing/score-profiles/${id}`,
        method: 'DELETE'
      }),
      // Delete cascades to snapshots on the server.
      invalidatesTags: (_r, _e, id) => [
        { type: 'MarketingScoreProfile', id },
        { type: 'MarketingScoreProfile', id: 'LIST' },
        { type: 'MarketingScoreSnapshot', id: `profile:${id}` }
      ]
    }),
    getProfileLeaderboard: builder.query<
      PaginatedItems<LeaderboardEntry>,
      { id: string; applicableOnly?: boolean; limit?: number; skip?: number }
    >({
      query: ({ id, ...rest }) =>
        `/v1/marketing/score-profiles/${id}/leaderboard${buildQS(rest)}`,
      providesTags: (_r, _e, { id }) => [
        { type: 'MarketingScoreSnapshot', id: `profile:${id}` }
      ]
    }),

    // --- Phase 2: Score Snapshots --------------------------------------

    listPersonScores: builder.query<SimpleItems<ScoreSnapshot>, string>({
      query: personId => `/v1/marketing/persons/${personId}/scores`,
      providesTags: (result, _e, personId) =>
        result?.items
          ? [
              ...result.items.map(({ uuid }) => ({
                type: 'MarketingScoreSnapshot' as const,
                id: uuid
              })),
              { type: 'MarketingScoreSnapshot', id: `person:${personId}` }
            ]
          : [{ type: 'MarketingScoreSnapshot', id: `person:${personId}` }]
    }),
    getScoreSnapshot: builder.query<ScoreSnapshot, string>({
      query: id => `/v1/marketing/score-snapshots/${id}`,
      providesTags: (_r, _e, id) => [{ type: 'MarketingScoreSnapshot', id }]
    }),

    // --- Phase 3: Conflict review queue --------------------------------

    listConflictReviews: builder.query<
      PaginatedItems<ConflictReview>,
      | {
          status?: ConflictReviewStatus;
          targetKind?: ConflictTargetKind;
          importJobUuid?: string;
          existingUuid?: string;
          limit?: number;
          skip?: number;
        }
      | undefined
    >({
      query: params =>
        `/v1/marketing/conflict-reviews${params ? buildQS(params) : ''}`,
      providesTags: result =>
        result?.items
          ? [
              ...result.items.map(({ uuid }) => ({
                type: 'MarketingConflictReview' as const,
                id: uuid
              })),
              { type: 'MarketingConflictReview', id: 'LIST' }
            ]
          : [{ type: 'MarketingConflictReview', id: 'LIST' }]
    }),
    getConflictReview: builder.query<ConflictReview, string>({
      query: id => `/v1/marketing/conflict-reviews/${id}`,
      providesTags: (_r, _e, id) => [{ type: 'MarketingConflictReview', id }]
    }),
    resolveConflictReview: builder.mutation<
      ConflictReview,
      { id: string; body: ResolveConflictPayload; importJobUuid?: string }
    >({
      query: ({ id, body }) => ({
        url: `/v1/marketing/conflict-reviews/${id}/resolve`,
        method: 'POST',
        body
      }),
      // Resolving may transition the parent job from
      // paused_for_review → done; invalidate the import-job entry too
      // so the imports list refreshes the badge column.
      invalidatesTags: (_r, _e, { id, importJobUuid }) => {
        const tags: Array<
          | { type: 'MarketingConflictReview'; id: string }
          | { type: 'MarketingImport'; id: string }
        > = [
          { type: 'MarketingConflictReview', id },
          { type: 'MarketingConflictReview', id: 'LIST' }
        ];
        if (importJobUuid) {
          tags.push({ type: 'MarketingImport', id: importJobUuid });
        }
        return tags;
      }
    }),
    dismissConflictReview: builder.mutation<
      ConflictReview,
      { id: string; body: DismissConflictPayload; importJobUuid?: string }
    >({
      query: ({ id, body }) => ({
        url: `/v1/marketing/conflict-reviews/${id}/dismiss`,
        method: 'POST',
        body
      }),
      invalidatesTags: (_r, _e, { id, importJobUuid }) => {
        const tags: Array<
          | { type: 'MarketingConflictReview'; id: string }
          | { type: 'MarketingImport'; id: string }
        > = [
          { type: 'MarketingConflictReview', id },
          { type: 'MarketingConflictReview', id: 'LIST' }
        ];
        if (importJobUuid) {
          tags.push({ type: 'MarketingImport', id: importJobUuid });
        }
        return tags;
      }
    })
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
  useGetMarketingImportQuery,
  // Phase 2
  useListPersonActivitiesQuery,
  useCreateActivityMutation,
  useCorrectActivityMutation,
  useListScoreProfilesQuery,
  useGetScoreProfileQuery,
  useCreateScoreProfileMutation,
  useReplaceScoreProfileMutation,
  useDeleteScoreProfileMutation,
  useGetProfileLeaderboardQuery,
  useListPersonScoresQuery,
  useGetScoreSnapshotQuery,
  // Phase 3 — conflict review queue
  useListConflictReviewsQuery,
  useGetConflictReviewQuery,
  useResolveConflictReviewMutation,
  useDismissConflictReviewMutation
} = marketingApi;
