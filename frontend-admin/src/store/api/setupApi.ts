import { baseApi, getTenantScopedTags } from './baseApi';
import { authApi } from './authApi';
import type { BackendUser } from './authApi';

/**
 * Drives the first-install onboarding wizard. The status + finalization-
 * access probes live at /v1/setup/* on the backend; /v1/setup/status is
 * unauthenticated (gated by the "no users exist yet" invariant enforced
 * server-side), the rest require an authenticated operator once the first
 * administrator exists.
 */

export interface SetupStatus {
  setupCompleted: boolean;
  /** Authoritative phase — 'complete' is what setupCompleted derives from. */
  phase: 'admin_required' | 'tenant_required' | 'complete';
  smtpConfigured: boolean;
}

export interface CreateAdminInput {
  email: string;
  password: string;
  fullName: string;
}

export interface CreateAdminResponse {
  success: boolean;
  accessToken: string;
  tokenType: string;
  expiresIn: number;
  user: BackendUser;
}

// Reports whether the calling operator may finalize the in-progress setup
// saga, or claim recovery of an unusable binding. Deliberately exposes no
// identity of the bound administrator — `reason` only.
export interface FinalizationAccess {
  canFinalize: boolean;
  canClaimRecovery: boolean;
  reason: '' | 'bound_to_another_admin' | 'recovery_requires_super_admin';
}

export interface FinalizeSetupInput {
  tenantName: string;
  tenantSlug: string;
  allowAdditionalInternalTenants: boolean;
}

export interface FinalizeSetupResult {
  /** Present only on a 202 — an identical request already holds the lease. */
  state?: 'setup.finalization_in_progress';
  tenantId?: string;
  tenantName?: string;
  tenantSlug?: string;
  mode?: 'manual' | 'single';
  allowAdditionalInternalTenants?: boolean;
}

export const setupApi = baseApi.injectEndpoints({
  endpoints: builder => ({
    // Lightweight status probe. Used by SetupGate on app boot and again after
    // the wizard completes so the gate can stop redirecting.
    getSetupStatus: builder.query<SetupStatus, void>({
      query: () => '/v1/setup/status',
      providesTags: ['Setup'],
      // Cache for longer than the default — the underlying state only flips
      // once per deployment. Both phase boundaries invalidate explicitly:
      // createInitialAdmin already does, finalizeSetup does below.
      keepUnusedDataFor: 300
    }),

    // Create the first administrator. Returns a full login response so the
    // caller can dispatch the standard auth slice `login` action and the
    // remaining wizard steps run authenticated.
    createInitialAdmin: builder.mutation<CreateAdminResponse, CreateAdminInput>(
      {
        query: body => ({
          url: '/v1/setup/admin',
          method: 'POST',
          body
        }),
        // A successful admin creation flips the phase forward; invalidate
        // the status cache so any subscribed SetupGate re-checks immediately.
        invalidatesTags: ['Setup', 'Auth', 'User', 'Navigation']
      }
    ),

    // Authenticated-operator probe: may this caller finalize setup, or claim
    // recovery? Authorization state — never serve stale.
    getFinalizationAccess: builder.query<FinalizationAccess, void>({
      query: () => '/v1/setup/finalization-access',
      providesTags: ['Setup'],
      keepUnusedDataFor: 0
    }),

    // Drives the resumable default-tenant-provisioning saga. On success
    // (200) the administrator's access token — minted before the Tier-1
    // tenant and owner membership existed — is stale: its `mbr` claim
    // doesn't list the new membership, so any X-Tenant-ID request would be
    // rejected by tenant resolution. NO invalidatesTags here: invalidation
    // is sequenced manually below so no tenant-scoped refetch can fire
    // before the re-minted JWT (with the new owner membership + the
    // platform-default fallback) is in the store.
    //
    // A 202 (`{state: 'setup.finalization_in_progress'}`) means an
    // identical request already holds the saga's stage lease — that is a
    // SUCCESS, not an error envelope. No re-mint has happened yet, so no
    // refresh and no invalidation: the component honors Retry-After and
    // retries the identical payload.
    finalizeSetup: builder.mutation<FinalizeSetupResult, FinalizeSetupInput>({
      query: body => ({ url: '/v1/setup/finalize', method: 'POST', body }),
      async onQueryStarted(_arg, { dispatch, queryFulfilled }) {
        let result;
        try {
          result = await queryFulfilled;
        } catch {
          return; // errors surface to the component
        }
        if (result.data?.state === 'setup.finalization_in_progress') {
          return; // 202: no re-mint, the component honors Retry-After
        }
        // 200 (first success or matching replay): force the session re-mint
        // BEFORE invalidating anything. getSession's queryFn dispatches
        // setAccessToken internally before resolving (authApi.ts:727-734),
        // so a non-null resolution with an accessToken means the
        // membership-bearing JWT is already in the store by the time we
        // get here.
        try {
          const session = await dispatch(
            authApi.endpoints.getSession.initiate(undefined, {
              forceRefetch: true
            })
          ).unwrap();
          if (!session?.accessToken) {
            // Blocking: component stays in the refresh-session state. Do
            // NOT invalidate Setup/Membership/Org/Navigation/tenant-scoped
            // tags here — that would let a tenant-scoped refetch fire
            // against the stale token.
            return;
          }
        } catch {
          return;
        }
        // Deliberately NOT invalidating 'Auth' here — getSession already
        // refreshed it above; invalidating it again would trigger a second
        // concurrent refresh and rotate the cookie a second time.
        dispatch(
          baseApi.util.invalidateTags([
            'Setup',
            'Membership',
            'Org',
            'Navigation',
            ...getTenantScopedTags()
          ])
        );
      }
    })
  })
});

export const {
  useGetSetupStatusQuery,
  useCreateInitialAdminMutation,
  useGetFinalizationAccessQuery,
  useFinalizeSetupMutation
} = setupApi;
