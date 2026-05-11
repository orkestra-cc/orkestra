// Non-sensitive "I think I have a session" marker stored in localStorage.
// The actual refresh token is httpOnly + Domain-scoped to the API origin
// so the SPA can never read it directly; this marker only signals
// "previously called signIn — worth attempting a silent refresh on next
// page load." Anonymous visitors have no marker and skip the refresh
// entirely, which avoids a guaranteed-401 round-trip on every cold load.
//
// Cleared on signOut, on a refresh that returns 401, and on any 4xx from
// the refresh endpoint — never trust a stale marker to keep retrying.

const KEY = 'orkestra_client_session_marker';

export function hasSessionMarker(): boolean {
  try {
    return globalThis.localStorage?.getItem(KEY) === '1';
  } catch {
    // localStorage can throw in private mode / SSR — treat as anonymous.
    return false;
  }
}

export function setSessionMarker(): void {
  try {
    globalThis.localStorage?.setItem(KEY, '1');
  } catch {
    // best-effort — failure means future refreshes won't auto-fire,
    // which is acceptable degradation.
  }
}

export function clearSessionMarker(): void {
  try {
    globalThis.localStorage?.removeItem(KEY);
  } catch {
    // best-effort
  }
}
