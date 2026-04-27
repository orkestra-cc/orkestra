import { baseApi } from './baseApi';
import { authApi } from './authApi';
import { setAccessToken } from '../slices/authSlice';
import type { Membership, EffectivePermissions } from '../slices/tenantSlice';

/**
 * Tenant lifecycle status — mirrors models.TenantStatus on the backend.
 * The canonical transitions are:
 *   provisioning → active ↔ suspended
 *                           ↘ archived → purged (terminal, crypto-shred)
 */
export type TenantStatus =
  | 'provisioning'
  | 'active'
  | 'suspended'
  | 'archived'
  | 'purged';

export interface Org {
  id: string;
  name: string;
  slug: string;
  ownerUserUUID: string;
  plan: string;
  features: string[];
  settings?: Record<string, string>;
  createdAt: string;
  updatedAt: string;
  /** Preferred state flag (see TenantStatus). Legacy rows may omit it. */
  status?: TenantStatus;
  /** Tenant tier — 'internal' (operator) or 'external' (client). */
  kind?: 'internal' | 'external';
  /** Set by the platform admin purge flow. Crypto-shred timestamp. */
  purgedAt?: string | null;
  /** Soft-archive timestamp. Succeeds the deprecated `deletedAt`. */
  archivedAt?: string | null;
}

export interface CreateOrgInput {
  name: string;
  slug: string;
  plan?: string;
  /**
   * Tenant tier. Defaults to 'internal' on the backend when omitted. The
   * two-tier admin UIs pass this explicitly so Internal Tenants and
   * Clients can never be conflated even if the request is crafted by hand.
   */
  kind?: 'internal' | 'external';
  /** Optional parent for external sub-tenants (divisions). */
  parentTenantUUID?: string;
}

export interface CreateDivisionInput {
  name: string;
  slug?: string;
}

/**
 * Flat read-only projection of a tenant's subscription, as served by
 * GET /v1/admin/tenants/{id}/subscriptions. The full Subscription shape
 * lives under the subscriptions module; this DTO is what the tenant
 * aggregator returns.
 */
export interface TenantSubscription {
  uuid: string;
  tenantUUID: string;
  serviceUUID: string;
  tierCode: string;
  status: string;
  currentPeriodStart: string;
  currentPeriodEnd: string;
  nextBillingAt: string;
  createdAt: string;
}

/** Flat read-only projection of a tenant's payment transaction. */
export interface TenantPayment {
  uuid: string;
  tenantUUID: string;
  subscriptionUUID: string;
  invoiceUUID: string;
  provider: string;
  providerTxID: string;
  status: string;
  amountCents: number;
  currency: string;
  refundedCents: number;
  chargedAt?: string | null;
  refundedAt?: string | null;
  createdAt: string;
}

/**
 * Flat read-only projection of a tenant's linked FatturaPA customer
 * (ADR-0001 PR-4). Returned by GET /v1/admin/tenants/{id}/billing-customer.
 * The full Customer shape lives under the billing module — this DTO is
 * what the tenant aggregator returns for the admin clients page.
 */
export interface TenantBillingCustomer {
  uuid: string;
  tenantUUID: string;
  denomination: string;
  name: string;
  surname: string;
  fiscalIdCode: string;
  isCompany: boolean;
  country: string;
  isActive: boolean;
  createdAt: string;
}

export interface AdminOrgListQuery {
  includeDeleted?: boolean;
  /** Filter by tenant tier. Omit for "both". */
  kind?: 'internal' | 'external';
  /** Return only direct children of the given parent tenant. */
  parentTenantUUID?: string;
  /** Shorthand: exclude every tenant that has a parent (root-level only). */
  rootsOnly?: boolean;
}

export interface UpdatePlanInput {
  plan: string;
  features: string[];
}

export interface CreateInviteInput {
  email: string;
  roles: string[];
}

export interface Invite {
  id: string;
  tenantId: string;
  email: string;
  roles: string[];
  invitedBy: string;
  createdAt: string;
  expiresAt: string;
  // Raw token — returned only on create, never on list. Treat as
  // unrecoverable after the first response is discarded.
  token?: string;
  acceptedAt?: string | null;
}

export interface Role {
  id: string;
  tenantId: string;
  name: string;
  description: string;
  permissions: string[];
  isSystem: boolean;
  isActive: boolean;
}

export interface UpdateRoleInput {
  name?: string;
  description?: string;
  permissions?: string[];
  isActive?: boolean;
}

export interface Binding {
  id: string;
  userUUID: string;
  tenantId: string;
  roleId: string;
  roleName: string;
  grantedBy?: string;
  grantedAt: string;
  expiresAt?: string | null;
}

export interface Permission {
  key: string;
  module: string;
  description: string;
  system: boolean;
}

export interface UpdateOrgAdminInput {
  name?: string;
  slug?: string;
  settings?: Record<string, string>;
}

export interface AdminOrgListItem extends Org {
  memberCount: number;
  deletedAt?: string | null;
}

export interface MembershipRecord {
  id: string;
  userUUID: string;
  tenantId: string;
  roles: string[];
  isOwner: boolean;
  invitedBy?: string;
  joinedAt: string;
  expiresAt?: string | null;
}

export const tenantApi = baseApi.injectEndpoints({
  endpoints: (builder) => ({
    // --- Org lifecycle ---
    listMyOrgs: builder.query<{ memberships: Membership[] }, void>({
      query: () => ({ url: '/v1/tenants', method: 'GET' }),
      providesTags: ['Membership']
    }),

    createOrg: builder.mutation<Org, CreateOrgInput>({
      query: (body) => ({ url: '/v1/tenants', method: 'POST', body }),
      invalidatesTags: ['Membership', 'Org'],
      // The caller's current JWT was issued before this tenant existed, so
      // its `mbr` claim does not list the new membership — every subsequent
      // X-Tenant-ID request for this tenant would be rejected by the
      // tenant-resolution middleware. Refresh the access token via /v1/auth/session
      // (which uses the HttpOnly refresh cookie) so the re-issued JWT carries
      // the freshly-created membership. Critical for the first-install
      // wizard, where the admin has no memberships at all when step 2 issues
      // its token.
      async onQueryStarted(_arg, { dispatch, queryFulfilled }) {
        try {
          await queryFulfilled;
        } catch {
          return;
        }
        try {
          const session = await dispatch(
            authApi.endpoints.getSession.initiate(undefined, { forceRefetch: true })
          ).unwrap();
          if (session?.accessToken) {
            dispatch(
              setAccessToken({
                accessToken: session.accessToken,
                expiresIn: session.expiresIn,
              })
            );
          }
        } catch {
          // Non-fatal: the user can still recover with a full page reload.
        }
      },
    }),

    getOrg: builder.query<Org, string>({
      query: (tenantId) => ({ url: `/v1/tenants/${tenantId}`, method: 'GET' }),
      providesTags: (_, __, id) => [{ type: 'Org', id }]
    }),

    updatePlan: builder.mutation<Org, { tenantId: string; body: UpdatePlanInput }>({
      query: ({ tenantId, body }) => ({ url: `/v1/tenants/${tenantId}/plan`, method: 'PATCH', body }),
      invalidatesTags: (_, __, { tenantId }) => [{ type: 'Org', id: tenantId }]
    }),

    // --- Members + invites ---
    listMembers: builder.query<{ members: Binding[] }, string>({
      query: (tenantId) => ({ url: `/v1/tenants/${tenantId}/members`, method: 'GET' }),
      providesTags: (_, __, tenantId) => [{ type: 'Membership', id: tenantId }]
    }),

    createInvite: builder.mutation<Invite, { tenantId: string; body: CreateInviteInput }>({
      query: ({ tenantId, body }) => ({ url: `/v1/tenants/${tenantId}/invites`, method: 'POST', body }),
      invalidatesTags: ['Membership']
    }),

    acceptInvite: builder.mutation<Org, { token: string }>({
      query: (body) => ({ url: '/v1/tenants/accept-invite', method: 'POST', body }),
      invalidatesTags: ['Membership']
    }),

    // --- Authz ---
    listPermissions: builder.query<{ permissions: Permission[] }, void>({
      query: () => ({ url: '/v1/authz/permissions', method: 'GET' }),
      providesTags: ['Permission']
    }),

    listRoles: builder.query<{ roles: Role[] }, string>({
      query: (tenantId) => ({ url: `/v1/tenants/${tenantId}/authz/roles`, method: 'GET' }),
      providesTags: ['Role']
    }),

    createRole: builder.mutation<Role, { tenantId: string; body: Omit<Role, 'id' | 'tenantId' | 'isSystem' | 'isActive'> }>({
      query: ({ tenantId, body }) => ({ url: `/v1/tenants/${tenantId}/authz/roles`, method: 'POST', body }),
      invalidatesTags: ['Role']
    }),

    updateRole: builder.mutation<Role, { tenantId: string; roleId: string; body: UpdateRoleInput }>({
      query: ({ tenantId, roleId, body }) => ({
        url: `/v1/tenants/${tenantId}/authz/roles/${roleId}`,
        method: 'PATCH',
        body,
      }),
      // Flipping isActive or editing permissions changes what every bound
      // user receives, so drop the effective-permissions cache as well.
      invalidatesTags: ['Role', 'EffectivePermissions'],
    }),

    deleteRole: builder.mutation<void, { tenantId: string; roleId: string }>({
      query: ({ tenantId, roleId }) => ({ url: `/v1/tenants/${tenantId}/authz/roles/${roleId}`, method: 'DELETE' }),
      // Cascades bindings on the backend — drop Binding + EffectivePermissions too.
      invalidatesTags: ['Role', 'Binding', 'EffectivePermissions'],
    }),

    listBindings: builder.query<{ bindings: Binding[] }, string>({
      query: (tenantId) => ({ url: `/v1/tenants/${tenantId}/authz/bindings`, method: 'GET' }),
      providesTags: ['Binding']
    }),

    createBinding: builder.mutation<Binding, { tenantId: string; body: { userUUID: string; roleId: string; expiresAt?: string } }>({
      query: ({ tenantId, body }) => ({ url: `/v1/tenants/${tenantId}/authz/bindings`, method: 'POST', body }),
      invalidatesTags: ['Binding', 'EffectivePermissions']
    }),

    deleteBinding: builder.mutation<void, { tenantId: string; bindingId: string }>({
      query: ({ tenantId, bindingId }) => ({ url: `/v1/tenants/${tenantId}/authz/bindings/${bindingId}`, method: 'DELETE' }),
      invalidatesTags: ['Binding', 'EffectivePermissions']
    }),

    getEffectivePermissions: builder.query<EffectivePermissions, string>({
      query: (tenantId) => ({ url: `/v1/tenants/${tenantId}/authz/me`, method: 'GET' }),
      providesTags: (_, __, tenantId) => [{ type: 'EffectivePermissions', id: tenantId }]
    }),

    // --- Platform admin tenant management (system.tenants.admin) ---
    listAllOrgsAdmin: builder.query<{ tenants: AdminOrgListItem[] }, AdminOrgListQuery | void>({
      query: (arg) => {
        const params: Record<string, string | boolean> = {};
        if (arg?.includeDeleted) params.includeDeleted = true;
        if (arg?.kind) params.kind = arg.kind;
        if (arg?.rootsOnly) params.rootsOnly = true;
        if (arg?.parentTenantUUID) params.parentTenantUUID = arg.parentTenantUUID;
        return {
          url: '/v1/admin/tenants',
          method: 'GET',
          params: Object.keys(params).length > 0 ? params : undefined,
        };
      },
      providesTags: (result) =>
        result
          ? [
              { type: 'AdminOrg', id: 'LIST' },
              ...result.tenants.map((o) => ({ type: 'AdminOrg' as const, id: o.id })),
            ]
          : [{ type: 'AdminOrg', id: 'LIST' }],
    }),

    // --- Phase 2 aggregators (divisions, subscriptions, payments per tenant) ---
    listTenantDivisionsAdmin: builder.query<{ divisions: Org[] }, string>({
      query: (tenantId) => ({
        url: `/v1/admin/tenants/${tenantId}/divisions`,
        method: 'GET',
      }),
      providesTags: (_, __, tenantId) => [{ type: 'AdminOrg', id: `${tenantId}:divisions` }],
    }),

    createTenantDivisionAdmin: builder.mutation<Org, { tenantId: string; body: CreateDivisionInput }>({
      query: ({ tenantId, body }) => ({
        url: `/v1/admin/tenants/${tenantId}/divisions`,
        method: 'POST',
        body,
      }),
      invalidatesTags: (_, __, { tenantId }) => [
        { type: 'AdminOrg', id: `${tenantId}:divisions` },
        { type: 'AdminOrg', id: 'LIST' },
      ],
    }),

    listTenantSubscriptionsAdmin: builder.query<
      { subscriptions: TenantSubscription[] },
      string
    >({
      query: (tenantId) => ({
        url: `/v1/admin/tenants/${tenantId}/subscriptions`,
        method: 'GET',
      }),
      providesTags: (_, __, tenantId) => [{ type: 'AdminOrg', id: `${tenantId}:subs` }],
    }),

    listTenantPaymentsAdmin: builder.query<{ payments: TenantPayment[] }, string>({
      query: (tenantId) => ({
        url: `/v1/admin/tenants/${tenantId}/payments`,
        method: 'GET',
      }),
      providesTags: (_, __, tenantId) => [{ type: 'AdminOrg', id: `${tenantId}:payments` }],
    }),

    // ADR-0001 PR-4 — billing-customer aggregator. 404 is the "not
    // linked" state and surfaces as `error.status === 404` on the
    // hook. The consumer (TenantDetailModal Billing tab) renders the
    // empty state when it sees that. Same pattern as identityApi's
    // getIdPConfig — keeps the type narrow and avoids a queryFn.
    getTenantBillingCustomerAdmin: builder.query<TenantBillingCustomer, string>({
      query: (tenantId) => ({
        url: `/v1/admin/tenants/${tenantId}/billing-customer`,
        method: 'GET',
      }),
      providesTags: (_, __, tenantId) => [
        { type: 'AdminOrg', id: `${tenantId}:billing-customer` },
      ],
    }),

    promoteTenantToBillingCustomerAdmin: builder.mutation<
      TenantBillingCustomer,
      string
    >({
      query: (tenantId) => ({
        url: `/v1/admin/tenants/${tenantId}/billing-customer`,
        method: 'POST',
      }),
      invalidatesTags: (_, __, tenantId) => [
        { type: 'AdminOrg', id: `${tenantId}:billing-customer` },
      ],
    }),

    getOrgAdmin: builder.query<Org, string>({
      query: (tenantId) => ({ url: `/v1/admin/tenants/${tenantId}`, method: 'GET' }),
      providesTags: (_, __, id) => [{ type: 'AdminOrg', id }],
    }),

    updateOrgAdmin: builder.mutation<Org, { tenantId: string; body: UpdateOrgAdminInput }>({
      query: ({ tenantId, body }) => ({ url: `/v1/admin/tenants/${tenantId}`, method: 'PATCH', body }),
      invalidatesTags: (_, __, { tenantId }) => [
        { type: 'AdminOrg', id: tenantId },
        { type: 'AdminOrg', id: 'LIST' },
      ],
    }),

    deleteOrgAdmin: builder.mutation<void, string>({
      query: (tenantId) => ({ url: `/v1/admin/tenants/${tenantId}`, method: 'DELETE' }),
      invalidatesTags: (_, __, tenantId) => [
        { type: 'AdminOrg', id: tenantId },
        { type: 'AdminOrg', id: 'LIST' },
      ],
    }),

    // Platform-admin purge — irreversible. Crypto-shreds the tenant's KMS
    // key; downstream data becomes mathematically unrecoverable even if
    // the ciphertext rows linger. Gated at the route level by
    // system.tenants.admin.
    purgeOrgAdmin: builder.mutation<void, string>({
      query: (tenantId) => ({
        url: `/v1/admin/tenants/${tenantId}/purge`,
        method: 'POST',
      }),
      invalidatesTags: (_, __, tenantId) => [
        { type: 'AdminOrg', id: tenantId },
        { type: 'AdminOrg', id: 'LIST' },
      ],
    }),

    updateOrgPlanAdmin: builder.mutation<Org, { tenantId: string; body: UpdatePlanInput }>({
      query: ({ tenantId, body }) => ({ url: `/v1/admin/tenants/${tenantId}/plan`, method: 'PATCH', body }),
      invalidatesTags: (_, __, { tenantId }) => [
        { type: 'AdminOrg', id: tenantId },
        { type: 'AdminOrg', id: 'LIST' },
      ],
    }),

    listOrgMembersAdmin: builder.query<{ members: MembershipRecord[] }, string>({
      query: (tenantId) => ({ url: `/v1/admin/tenants/${tenantId}/members`, method: 'GET' }),
      providesTags: (_, __, tenantId) => [{ type: 'Membership', id: tenantId }],
    }),

    removeOrgMemberAdmin: builder.mutation<void, { tenantId: string; userUUID: string }>({
      query: ({ tenantId, userUUID }) => ({
        url: `/v1/admin/tenants/${tenantId}/members/${userUUID}`,
        method: 'DELETE',
      }),
      invalidatesTags: (_, __, { tenantId }) => [
        { type: 'Membership', id: tenantId },
        { type: 'AdminOrg', id: tenantId },
        { type: 'AdminOrg', id: 'LIST' },
      ],
    }),

    listOrgInvitesAdmin: builder.query<
      { invites: Invite[] },
      { tenantId: string; includeAccepted?: boolean }
    >({
      query: ({ tenantId, includeAccepted }) => ({
        url: `/v1/admin/tenants/${tenantId}/invites`,
        method: 'GET',
        params: includeAccepted ? { includeAccepted: true } : undefined,
      }),
      providesTags: (_, __, { tenantId }) => [{ type: 'OrgInvite', id: tenantId }],
    }),

    createOrgInviteAdmin: builder.mutation<Invite, { tenantId: string; body: CreateInviteInput }>({
      query: ({ tenantId, body }) => ({ url: `/v1/admin/tenants/${tenantId}/invites`, method: 'POST', body }),
      invalidatesTags: (_, __, { tenantId }) => [{ type: 'OrgInvite', id: tenantId }],
    }),

    revokeOrgInviteAdmin: builder.mutation<void, { tenantId: string; inviteId: string }>({
      query: ({ tenantId, inviteId }) => ({
        url: `/v1/admin/tenants/${tenantId}/invites/${inviteId}`,
        method: 'DELETE',
      }),
      invalidatesTags: (_, __, { tenantId }) => [{ type: 'OrgInvite', id: tenantId }],
    }),
  }),
  overrideExisting: false,
});

export const {
  useListMyOrgsQuery,
  useCreateOrgMutation,
  useGetOrgQuery,
  useUpdatePlanMutation,
  useListMembersQuery,
  useCreateInviteMutation,
  useAcceptInviteMutation,
  useListPermissionsQuery,
  useListRolesQuery,
  useCreateRoleMutation,
  useUpdateRoleMutation,
  useDeleteRoleMutation,
  useListBindingsQuery,
  useCreateBindingMutation,
  useDeleteBindingMutation,
  useGetEffectivePermissionsQuery,
  useListAllOrgsAdminQuery,
  useGetOrgAdminQuery,
  useUpdateOrgAdminMutation,
  useDeleteOrgAdminMutation,
  usePurgeOrgAdminMutation,
  useUpdateOrgPlanAdminMutation,
  useListOrgMembersAdminQuery,
  useRemoveOrgMemberAdminMutation,
  useListOrgInvitesAdminQuery,
  useCreateOrgInviteAdminMutation,
  useRevokeOrgInviteAdminMutation,
  useListTenantDivisionsAdminQuery,
  useCreateTenantDivisionAdminMutation,
  useListTenantSubscriptionsAdminQuery,
  useListTenantPaymentsAdminQuery,
  useGetTenantBillingCustomerAdminQuery,
  usePromoteTenantToBillingCustomerAdminMutation,
} = tenantApi;
