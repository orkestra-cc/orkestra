// Module-scoped in-memory access token. Stored outside React state so the
// API client middleware can read it synchronously without dragging React
// context into the fetch path. The refresh cookie is httpOnly and lives
// on the API origin (Domain=api.localhost in dev, api.<env>.orkestra.cc
// in staging/prod) — the SPA never sees it directly; it just calls the
// refresh endpoint and stores the resulting access token here.

import {
  clearSessionMarker,
  hasSessionMarker,
  setSessionMarker,
} from "@/auth/sessionMarker";
import { jwtExp } from "@/lib/jwtExp";

let accessToken: string | null = null;
// The moment the current token expires, in the Date.now() domain — recorded
// from the DURATION the server reported at receipt, not from the token's
// absolute `exp`. Both ends of the eventual comparison then come from the same
// clock, so a constant offset cancels and clock skew stops being a failure
// mode rather than merely being tolerated (§4.5). `null` means UNKNOWN, which
// §4.3 branch 2 treats as LIVE.
let accessTokenExpiresAt: number | null = null;
const subscribers = new Set<(token: string | null) => void>();

export function getAccessToken(): string | null {
  return accessToken;
}

// The token and what we know about its life are ONE fact and must be read
// together: a 401 handler that read them separately could compare a token it
// sent against an expiry a sibling request installed moments later.
export function getAccessTokenSnapshot(): {
  token: string | null;
  expiresAt: number | null;
} {
  return { token: accessToken, expiresAt: accessTokenExpiresAt };
}

// expiresInSeconds is the server's own figure (LoginResult.expiresIn,
// MfaLoginVerifyResult.expiresIn, the refresh body). It is OPTIONAL, and an
// absent one is not an error: the JWT's own `exp` is tried next, and an
// unreadable one leaves the expiry unknown.
export function setAccessToken(
  token: string | null,
  expiresInSeconds?: number,
): void {
  accessToken = token;
  accessTokenExpiresAt = resolveExpiresAt(token, expiresInSeconds);
  for (const fn of subscribers) fn(token);
}

function resolveExpiresAt(
  token: string | null,
  expiresInSeconds?: number,
): number | null {
  if (token === null) return null;
  // Number.isFinite guards the same hazard jwtExp does: a body carrying
  // `expiresIn: 1e400` would otherwise record a token that never expires.
  if (
    typeof expiresInSeconds === "number" &&
    Number.isFinite(expiresInSeconds)
  ) {
    return Date.now() + expiresInSeconds * 1000;
  }
  return jwtExp(token);
}

export function clearAccessToken(): void {
  setAccessToken(null);
}

export function subscribe(fn: (token: string | null) => void): () => void {
  subscribers.add(fn);
  return () => {
    subscribers.delete(fn);
  };
}

/**
 * The outcome of a refresh attempt.
 *
 * `unavailable` is the case that must NOT be collapsed into `signed-out`.
 * The backend answers 503 `session_enforcement_unavailable` when it could
 * not *evaluate* the session — the durable store behind session enforcement
 * was unreachable. ADR-0017 gives that its own status precisely so a client
 * does not treat it as a sign-out: a repository outage "would train clients
 * to discard a session that is still perfectly valid." Clearing the token
 * and the session marker on a 503, as this used to, is exactly the behaviour
 * the 503 exists to prevent — and clearing the marker makes it sticky, since
 * the next cold load then short-circuits before even trying.
 */
export type RefreshOutcome =
  | { status: "ok"; accessToken: string }
  | { status: "signed-out" }
  | { status: "unavailable" };

// In-flight refresh promise — coalesces concurrent callers (a burst of
// 401s, a StrictMode double-invoked bootstrap, a bootstrap racing the
// cold-load refresh) into a single /v1/auth/client/refresh-cookie call, so
// the rotating refresh cookie is never presented twice at once.
let inflightRefresh: Promise<RefreshOutcome> | null = null;

const signedOut = (): RefreshOutcome => {
  clearSessionMarker();
  clearAccessToken();
  return { status: "signed-out" };
};

// performRefresh presents the refresh cookie once — coalesced, and
// UNCONDITIONAL: it never consults the session marker, so a storage that
// throws (private mode, disabled storage) cannot turn a valid cookie into a
// sign-out. It is the single place the outcome is decided:
//   ok           2xx with an access token → installed in memory (never stored)
//   signed-out   401, any other non-503 non-2xx, or a 2xx without a token →
//                marker and token cleared; there is nothing to retry
//   unavailable  503 (ADR-0017: the session could not be evaluated) or the
//                fetch itself rejecting (a transport failure) → token and
//                marker untouched; the caller may retry
async function performRefresh(apiBase: string): Promise<RefreshOutcome> {
  if (inflightRefresh) return inflightRefresh;
  inflightRefresh = (async (): Promise<RefreshOutcome> => {
    try {
      let res: Response;
      try {
        res = await fetch(`${apiBase}/v1/auth/client/refresh-cookie`, {
          method: "POST",
          credentials: "include",
        });
      } catch {
        return { status: "unavailable" };
      }
      if (res.status === 503) return { status: "unavailable" };
      if (!res.ok) return signedOut();
      // models.TokenResponse — we read accessToken from the body (a legacy
      // `token` key is tolerated).
      const body = (await res.json().catch(() => ({}))) as {
        accessToken?: string;
        token?: string;
        expiresIn?: number;
      };
      const fresh = body.accessToken ?? body.token ?? null;
      // A 2xx with no token is a broken response, not an outage.
      if (!fresh) return signedOut();
      setAccessToken(fresh, body.expiresIn);
      return { status: "ok", accessToken: fresh };
    } finally {
      inflightRefresh = null;
    }
  })();
  return inflightRefresh;
}

// refreshAccessToken — the AUTOMATIC path (cold-load rehydration in
// AuthProvider, the 401 middleware in api/client.ts). Skips the request
// entirely when no session marker is present (anonymous visitor): refresh
// cookies are httpOnly so the SPA cannot probe for them, and a
// guaranteed-401 on every cold load is console noise for every anonymous
// visitor. The marker is stamped on signIn (and by bootstrapFromRefreshCookie)
// and cleared on signOut / any signed-out outcome. Never rejects.
export async function refreshAccessToken(
  apiBase: string,
): Promise<RefreshOutcome> {
  if (!hasSessionMarker()) return { status: "signed-out" };
  return performRefresh(apiBase);
}

// bootstrapFromRefreshCookie adopts a refresh cookie the SPA did not set
// itself — the client-tier OAuth relay sets it on the API host and lands
// the browser on /auth/callback with nothing else (spec §4.10, §5 #23).
// The marker is stamped FIRST, speculatively, so the next cold load and any
// concurrent automatic refresh know a cookie exists; then the cookie is
// presented REGARDLESS of whether that stamp succeeded. `ok` keeps the
// marker; every `signed-out` shape clears it again; `unavailable` (503 or
// transport failure) keeps it so the caller can offer a retry. Never rejects.
export async function bootstrapFromRefreshCookie(
  apiBase: string,
): Promise<RefreshOutcome> {
  setSessionMarker();
  return performRefresh(apiBase);
}
