// Non-sensitive "I think I have a session" marker stored in localStorage.
// The actual refresh token is httpOnly + Domain-scoped to the API origin
// so the SPA can never read it directly; this marker only signals
// "previously called signIn — worth attempting a silent refresh on next
// page load." Anonymous visitors have no marker and skip the refresh
// entirely, which avoids a guaranteed-401 round-trip on every cold load.
//
// Cleared on signOut and on an EXPLICIT REJECTION, never on an outage (§4.1):
// a 401 — and only a 401 — from the refresh endpoint, or a 401 carrying a
// terminal session code (session_revoked, session_max_age_reached) on an
// authenticated request. A 429, a 5xx, a timeout or a 2xx without a token keep
// the marker: none of them says anything about the session, and clearing it
// there is STICKY — the next cold load short-circuits before even trying.

const KEY = "orkestra_client_session_marker";

export function hasSessionMarker(): boolean {
  try {
    return globalThis.localStorage?.getItem(KEY) === "1";
  } catch {
    // localStorage can throw in private mode / SSR — treat as anonymous.
    return false;
  }
}

export function setSessionMarker(): void {
  try {
    globalThis.localStorage?.setItem(KEY, "1");
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
