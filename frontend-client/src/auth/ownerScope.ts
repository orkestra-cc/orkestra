// Owner-scope state for the Tier-2 dashboard. Every /v1/me/* read fans
// out across the caller's user identity plus every tenant they own
// (server-side: see ClientHandler.callerOwnerSet); this hook lets the
// SPA narrow that fan-out to one principal without breaking the
// "personal-by-default" UX.
//
//   { kind: 'all' }                    → no filter (default — fan out)
//   { kind: 'user' }                   → only the calling user
//   { kind: 'tenant', uuid: "..." }    → one owned tenant
//
// Persisted to localStorage so the choice survives navigation between
// dashboard pages. Reset to 'all' whenever the selected tenant is no
// longer in the JWT mbr claim (token rotated, membership revoked).
import { useCallback, useEffect, useState } from 'react';

import { useOwnedTenants, type JwtMembership } from '@/auth/memberships';

export type OwnerScope =
  | { kind: 'all' }
  | { kind: 'user' }
  | { kind: 'tenant'; uuid: string };

const STORAGE_KEY = 'client.ownerScope';
const ALL: OwnerScope = { kind: 'all' };

function readStored(): OwnerScope {
  if (typeof window === 'undefined') return ALL;
  try {
    const raw = window.localStorage.getItem(STORAGE_KEY);
    if (!raw) return ALL;
    const parsed = JSON.parse(raw) as OwnerScope;
    if (parsed.kind === 'all' || parsed.kind === 'user') return parsed;
    if (parsed.kind === 'tenant' && typeof parsed.uuid === 'string' && parsed.uuid)
      return parsed;
    return ALL;
  } catch {
    return ALL;
  }
}

function writeStored(scope: OwnerScope) {
  if (typeof window === 'undefined') return;
  try {
    window.localStorage.setItem(STORAGE_KEY, JSON.stringify(scope));
  } catch {
    // Storage may be disabled (private mode quotas, etc.) — the scope
    // still works in-memory for the current tab.
  }
}

interface OwnerScopeReturn {
  scope: OwnerScope;
  setScope: (next: OwnerScope) => void;
  ownedTenants: JwtMembership[];
  // True iff the caller has at least one owned tenant — drives the
  // switcher visibility (private-by-default when there are none).
  hasTenants: boolean;
}

export function useOwnerScope(): OwnerScopeReturn {
  const ownedTenants = useOwnedTenants();
  const [scope, setScopeState] = useState<OwnerScope>(() => readStored());

  // Drop a stale tenant scope when the JWT no longer carries the
  // matching membership (token rotated, admin revoked the grant).
  useEffect(() => {
    if (scope.kind !== 'tenant') return;
    const stillOwned = ownedTenants.some((m) => m.tenantUuid === scope.uuid);
    if (!stillOwned) {
      setScopeState(ALL);
      writeStored(ALL);
    }
  }, [ownedTenants, scope]);

  const setScope = useCallback((next: OwnerScope) => {
    setScopeState(next);
    writeStored(next);
  }, []);

  return { scope, setScope, ownedTenants, hasTenants: ownedTenants.length > 0 };
}

// Build the (ownerKind, ownerUuid) query params the /v1/me/* handlers
// expect, given the current scope and the caller's user UUID. The "all"
// case returns an empty object — the backend's callerOwnerSet then fans
// out across every owned principal.
export function ownerQuery(
  scope: OwnerScope,
  userUUID: string | undefined,
): Record<string, string> {
  switch (scope.kind) {
    case 'all':
      return {};
    case 'user':
      return userUUID ? { ownerKind: 'user', ownerUuid: userUUID } : {};
    case 'tenant':
      return { ownerKind: 'tenant', ownerUuid: scope.uuid };
  }
}

// Compact tenant label — the JWT mbr claim does not carry a tenant name
// and the client API surface intentionally does not expose /v1/tenants
// (operator-only). Until tenant names land on the client surface, fall
// back to a short UUID prefix so the switcher is at least disambiguating.
export function shortTenantLabel(uuid: string): string {
  return uuid.length > 8 ? uuid.slice(0, 8) : uuid;
}
