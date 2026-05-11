// Tier-2 self-service billing-identity wrappers.
//
// Phase 6 of the Unified Client Aggregate refactor (2026-05-08) collapsed
// the standalone clientbilling addon into the Tenant aggregate. The
// previous /v1/me/billing-profile endpoint is gone; this module now talks
// to the tenant module's /v1/me/billing-identity surface, which writes the
// billing-identity sub-document directly onto the caller's personal tenant
// (lazy-provisioned by EnsureTenantForUser on first access).
//
// The wire shape mirrors backend/internal/core/tenant/handlers/handler.go's
// BillingIdentityDTO. The `getBillingProfile` and `putBillingProfile`
// function names are preserved for source-level compatibility with
// callers that pre-flighted the legacy endpoint.
import { apiBaseURL } from '@/api/client';
import { getAccessToken } from '@/auth/tokenStore';

export interface BillingAddress {
  line1?: string;
  line2?: string;
  city?: string;
  province?: string;
  postalCode?: string;
  country?: string;
}

export interface FatturaPAProfile {
  codiceDestinatario?: string;
  pecDestinatario?: string;
  isPA?: boolean;
  codiceUfficio?: string;
  riferimentoAmm?: string;
  convenzioneNumero?: string;
}

export interface BillingIdentity {
  tenantId: string;
  isCompany: boolean;
  isItalianBillable: boolean;
  legalName?: string;
  vatNumber?: string;
  fiscalCode?: string;
  billingAddress?: BillingAddress;
  fatturaPA?: FatturaPAProfile;
}

export interface BillingProfileApiError extends Error {
  status: number;
  code?: string;
}

function err(message: string, status: number, code?: string): BillingProfileApiError {
  const e = new Error(message) as BillingProfileApiError;
  e.status = status;
  if (code) e.code = code;
  return e;
}

async function readError(res: Response, fallback: string): Promise<BillingProfileApiError> {
  const body = (await res.json().catch(() => ({}))) as {
    detail?: string;
    title?: string;
    code?: string;
  };
  return err(body.detail ?? body.title ?? fallback, res.status, body.code);
}

async function authedJson(path: string, init?: RequestInit): Promise<Response> {
  const token = getAccessToken();
  return fetch(`${apiBaseURL}${path}`, {
    credentials: 'include',
    ...init,
    headers: {
      'Content-Type': 'application/json',
      Accept: 'application/json',
      ...(token ? { Authorization: `Bearer ${token}` } : {}),
      ...(init?.headers ?? {}),
    },
  });
}

export async function getBillingProfile(signal?: AbortSignal): Promise<BillingIdentity> {
  const res = await authedJson('/v1/me/billing-identity', { method: 'GET', signal });
  if (!res.ok) throw await readError(res, 'Failed to load billing identity');
  return (await res.json()) as BillingIdentity;
}

export interface UpsertBillingProfileInput {
  isCompany: boolean;
  legalName?: string;
  vatNumber?: string;
  fiscalCode?: string;
  billingAddress?: BillingAddress;
  fatturaPA?: FatturaPAProfile;
}

export async function putBillingProfile(
  input: UpsertBillingProfileInput,
): Promise<BillingIdentity> {
  const res = await authedJson('/v1/me/billing-identity', {
    method: 'PATCH',
    body: JSON.stringify(input),
  });
  if (!res.ok) throw await readError(res, 'Failed to save billing identity');
  return (await res.json()) as BillingIdentity;
}

export async function setItalianBillable(enabled: boolean): Promise<BillingIdentity> {
  const res = await authedJson('/v1/me/italian-billable', {
    method: 'POST',
    body: JSON.stringify({ enabled }),
  });
  if (!res.ok) throw await readError(res, 'Failed to toggle Italian billable mode');
  return (await res.json()) as BillingIdentity;
}

// hasBillingProfile mirrors the personal-tenant invariant on the backend:
// a tenant has a usable billing identity when the country is set, and (for
// company tenants) a legal name is filled in. Natural-person tenants fall
// back on the owner User's name fields at invoice-render time, so we only
// require country in that branch. The endpoint always returns a row (the
// personal tenant is lazy-provisioned), so we infer "incomplete" from the
// fields rather than from a 404.
export function hasBillingProfile(p: BillingIdentity | null | undefined): boolean {
  if (!p) return false;
  if (!p.billingAddress?.country?.trim()) return false;
  if (p.isCompany) {
    return !!p.legalName?.trim();
  }
  return true;
}
