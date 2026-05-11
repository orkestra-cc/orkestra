// Decode the in-memory access token's `mbr` claim so the SPA can
// surface the caller's tenant memberships without an extra round trip
// (no `/v1/me/tenants` exists on the client surface — see ADR-0003 D-8;
// `/v1/tenants` is operator-only). The JWT payload uses compact keys
// from backend/internal/core/auth/services/jwt_service.go::claimsToMap:
//   mbr  → memberships array of { tid, k, r }
//   dtid → default tenant id
//   sub  → user uuid
//
// "Owned" tenants are inferred from the `org_owner` role (per
// backend/internal/core/tenant/CLAUDE.md — the first membership is
// inserted with Roles:["org_owner"]). The backend re-validates ownership
// against TenantProvider.ListUserMemberships on every /v1/me/* call, so
// this client-side filter is purely a UX hint.
import { useSyncExternalStore } from 'react';
import { getAccessToken, subscribe } from '@/auth/tokenStore';

export interface JwtMembership {
  tenantUuid: string;
  tenantKind: string;
  roles: string[];
  isOwner: boolean;
}

interface RawMembership {
  tid?: unknown;
  k?: unknown;
  r?: unknown;
}

function decodeBase64Url(input: string): string {
  const padded = input.replace(/-/g, '+').replace(/_/g, '/');
  const pad = padded.length % 4 === 0 ? '' : '='.repeat(4 - (padded.length % 4));
  // atob handles UTF-8 by yielding latin1 bytes — wrap with TextDecoder
  // so the resulting JSON parses correctly when claims contain non-ASCII.
  const binary = atob(padded + pad);
  const bytes = new Uint8Array(binary.length);
  for (let i = 0; i < binary.length; i += 1) bytes[i] = binary.charCodeAt(i);
  return new TextDecoder().decode(bytes);
}

export function decodeMemberships(token: string | null): JwtMembership[] {
  if (!token) return [];
  const parts = token.split('.');
  if (parts.length !== 3) return [];
  try {
    const payload = JSON.parse(decodeBase64Url(parts[1])) as { mbr?: unknown };
    if (!Array.isArray(payload.mbr)) return [];
    return payload.mbr
      .map((raw): JwtMembership | null => {
        const m = raw as RawMembership;
        const tenantUuid = typeof m.tid === 'string' ? m.tid : '';
        if (!tenantUuid) return null;
        const roles = Array.isArray(m.r) ? m.r.filter((r): r is string => typeof r === 'string') : [];
        return {
          tenantUuid,
          tenantKind: typeof m.k === 'string' ? m.k : '',
          roles,
          isOwner: roles.includes('org_owner'),
        };
      })
      .filter((m): m is JwtMembership => m !== null);
  } catch {
    return [];
  }
}

// useSyncExternalStore subscribes to the token store so the hook
// re-renders when the access token rotates (login, refresh, logout).
// Memoising decode result by token identity avoids re-parsing on every
// render — getSnapshot is called frequently by React.
let cachedToken: string | null | undefined;
let cachedMemberships: JwtMembership[] = [];

function getSnapshot(): JwtMembership[] {
  const token = getAccessToken();
  if (token !== cachedToken) {
    cachedToken = token;
    cachedMemberships = decodeMemberships(token);
  }
  return cachedMemberships;
}

function subscribeToToken(callback: () => void): () => void {
  return subscribe(() => callback());
}

export function useMemberships(): JwtMembership[] {
  return useSyncExternalStore(subscribeToToken, getSnapshot, getSnapshot);
}

export function useOwnedTenants(): JwtMembership[] {
  return useMemberships().filter((m) => m.isOwner);
}
