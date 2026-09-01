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
 * `unavailable` is the case that must NOT be collapsed into `signed-out`, and
 * it is now the DEFAULT: it covers 503 (`session_enforcement_unavailable` and
 * `refresh_lookup_unavailable`), 429 from the router's global rate limiter,
 * 408, every other 5xx and every other 4xx, a twice-raced 409, a 2xx that
 * carries no token, a transport failure and the 10s fetch timeout. All of
 * those say something about the SERVER and nothing about the session.
 *
 * The backend answers 503 when it could not *evaluate* the session — the
 * durable store behind session enforcement was unreachable. ADR-0017 gives
 * that its own status precisely so a client does not treat it as a sign-out:
 * a repository outage "would train clients to discard a session that is still
 * perfectly valid." Clearing the token and the session marker on a 503, as
 * this used to, is exactly the behaviour the 503 exists to prevent — and
 * clearing the marker makes it sticky, since the next cold load then
 * short-circuits before even trying.
 *
 * `signed-out` is an ALLOWLIST of exactly one status: 401, the one answer that
 * means "the credential I presented was rejected".
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

// The cross-tab rotation lock. Web Locks is the only cross-tab primitive that
// releases automatically when the holder navigates away or crashes. The name is
// shared with the operator console, but locks are PER-ORIGIN, so client.* and
// console.* never contend — and by ADR-0003 D-9 they hold different refresh
// cookies anyway.
export const REFRESH_LOCK_NAME = "orkestra:auth-refresh";

// The value frontend-admin settled on (baseApi.ts:84). This bound is what makes
// the unbounded Web Lock above safe: everything done while holding the lock
// happens inside a fetch timeout, so the lock is bounded TRANSITIVELY.
// The true hold is at most TWO of these — 2 x REFRESH_FETCH_TIMEOUT_MS — because
// the single 409 retry is a second full attempt inside the same lock; the
// PER-FETCH bound stays 10s. Weakening this re-arms the lock.
export const REFRESH_FETCH_TIMEOUT_MS = 10_000;

async function withRefreshLock(
  run: () => Promise<RefreshOutcome>,
): Promise<RefreshOutcome> {
  const locks = typeof navigator !== "undefined" ? navigator.locks : undefined;
  // `!locks?.request`, NOT `typeof locks === "undefined"`: happy-dom 20.x sets
  // navigator.locks to NULL, and `typeof null === "object"`, so the typeof
  // guard passes and then throws on `.request`. Read at CALL time, never
  // captured at module load — a console (and Task 10's manual probe) can null
  // it out between calls.
  if (!locks?.request) return run();
  // `granted` scopes the catch below to the ACQUISITION alone. request()
  // rejects for two unrelated reasons — the lock manager refusing to grant
  // (InvalidStateError when the document is not fully active, an
  // implementation that throws) and the callback itself throwing — and only
  // the first says nothing about the session. The callback never throws by
  // construction (attemptRefresh catches everything and every branch returns
  // a RefreshOutcome), so a rejection AFTER it started is a programming error
  // and is rethrown rather than silently reported as an outage.
  let granted = false;
  try {
    // Deliberately the 2-argument overload. Bounding the LOCK needs
    // request(name, {signal}, cb), and frontend-admin's own comment records
    // that switching shapes silently defeated its test. The bound comes from
    // the fetch timeout instead.
    return await locks.request(REFRESH_LOCK_NAME, () => {
      granted = true;
      return run();
    });
  } catch (err) {
    if (granted) throw err;
    // A lock we could not take is not an answer about the session, so by the
    // §4.1 allowlist it is `unavailable` — token and marker kept. It must not
    // propagate either: AuthProvider's mount-time call is
    // `void refreshAccessToken(...)`, so a rejection here would land as an
    // unhandled rejection and the documented "never rejects" contract on both
    // entry points would be false.
    return { status: "unavailable" };
  }
}

// A promise that rejects when the signal aborts and never resolves otherwise.
// `fetch` resolves on HEADERS, and the body stream of a mocked or proxied
// response does not always observe the request's abort signal (MSW's does not
// — measured), so the body read is raced against the signal EXPLICITLY. The
// transitive bound on the Web Lock must not depend on the platform propagating
// an abort into the body: a server that sends headers and then stalls would
// otherwise hold the lock — and the in-flight promise — for as long as it
// stalls. Nothing inspects the rejection value; it only unblocks the race.
function rejectOnAbort(signal: AbortSignal): Promise<never> {
  return new Promise<never>((_, reject) => {
    const fail = () => reject(new Error("refresh aborted"));
    if (signal.aborted) {
      fail();
      return;
    }
    signal.addEventListener("abort", fail, { once: true });
  });
}

type RefreshAttempt =
  | { kind: "ok"; accessToken: string; expiresIn?: number }
  | { kind: "raced" }
  | { kind: "signed-out" }
  | { kind: "unavailable" };

// One presentation of the refresh cookie.
//
// The outcome rule is an ALLOWLIST: 401 is the only status that means "the
// credential I presented was rejected". Everything else that is not a usable
// 2xx says something about the SERVER and nothing about the session, so it is
// `unavailable` and nothing is cleared (G2). A denylist is what defect C was —
// and /refresh-cookie sits under the router's GLOBAL rate limiter, so 429 is
// reachable on every refresh and a burst of tabs is exactly what trips it.
async function attemptRefresh(apiBase: string): Promise<RefreshAttempt> {
  const ctrl = new AbortController();
  // AbortController + setTimeout, NOT AbortSignal.timeout: the latter runs on
  // an internal timer vitest's fake clock does not control, so the timeout
  // case could not be tested the way a reader would expect. It also cannot be
  // cancelled, leaving a live 10s timer behind after every refresh.
  const timer = setTimeout(() => ctrl.abort(), REFRESH_FETCH_TIMEOUT_MS);
  try {
    const res = await fetch(`${apiBase}/v1/auth/client/refresh-cookie`, {
      method: "POST",
      credentials: "include",
      signal: ctrl.signal,
    });
    // 409 refresh_rotation_raced: a sibling rotated first and the family is
    // intact — our cookie jar already holds its successor.
    if (res.status === 409) return { kind: "raced" };
    if (res.status === 401) return { kind: "signed-out" };
    if (!res.ok) return { kind: "unavailable" };
    // models.TokenResponse — we read accessToken from the body (a legacy
    // `token` key is tolerated). The parse is neutralised BEFORE the race, so a
    // body that errors after the abort won the race cannot surface as an
    // unhandled rejection; racing it against the signal is what makes the
    // timer bound the READ as well as the fetch.
    const parsed = res.json().catch(() => ({}));
    const body = (await Promise.race([parsed, rejectOnAbort(ctrl.signal)])) as {
      accessToken?: string;
      token?: string;
      expiresIn?: number;
    };
    const fresh = body.accessToken ?? body.token ?? null;
    // A 2xx with no token is a BROKEN RESPONSE, which is the reason not to act
    // on it: it has told us nothing about the session.
    if (!fresh) return { kind: "unavailable" };
    return { kind: "ok", accessToken: fresh, expiresIn: body.expiresIn };
  } catch {
    // A transport failure, or the abort — including one that fires during the
    // body read, which is why clearTimeout is in the `finally` and NOWHERE
    // else. `fetch` resolves on HEADERS, so clearing the timer straight after
    // the await bounds almost nothing: a server that sends headers and stalls
    // the body would hold the Web Lock for as long as it stalls.
    // "No answer" is not "no".
    return { kind: "unavailable" };
  } finally {
    clearTimeout(timer);
  }
}

// performRefresh presents the refresh cookie — coalesced in-tab, serialised
// ACROSS tabs by the Web Lock, and UNCONDITIONAL: it never consults the
// session marker, so a storage that throws (private mode, disabled storage)
// cannot turn a valid cookie into a sign-out. It is the single place the
// outcome is decided, and the table is an ALLOWLIST:
//   ok           2xx with an access token → installed in memory (never stored)
//   signed-out   401, and ONLY 401 → marker and token cleared; nothing to retry
//   unavailable  everything else — 503, 429, 408, any other 5xx or 4xx, a
//                twice-raced 409, a 2xx without a token, a transport failure,
//                the 10s timeout → token and marker untouched; caller may retry
// A 409 `refresh_rotation_raced` is retried exactly once before it lands in
// `unavailable` — which is why the lock is held for at most TWO fetch timeouts
// (2 x REFRESH_FETCH_TIMEOUT_MS), the retry being a second full attempt inside
// it. A lock request that REJECTS is `unavailable` too, never a rejection. The
// in-flight promise wraps the lock, so a second in-tab caller shares the first
// one's answer instead of queueing behind the lock.
async function performRefresh(apiBase: string): Promise<RefreshOutcome> {
  if (inflightRefresh) return inflightRefresh;
  inflightRefresh = (async (): Promise<RefreshOutcome> => {
    try {
      return await withRefreshLock(async (): Promise<RefreshOutcome> => {
        let attempt = await attemptRefresh(apiBase);
        if (attempt.kind === "raced") {
          // A sibling won the CAS, so the browser already holds the successor
          // cookie and a second attempt lands. Exactly ONE retry: a race
          // surviving two attempts is far more likely a live session than a
          // dead one, and guessing "dead" is the failure this removes. The
          // marker is untouched on every 409 path.
          attempt = await attemptRefresh(apiBase);
          if (attempt.kind === "raced") return { status: "unavailable" };
        }
        if (attempt.kind === "signed-out") return signedOut();
        if (attempt.kind === "unavailable") return { status: "unavailable" };
        setAccessToken(attempt.accessToken, attempt.expiresIn);
        return { status: "ok", accessToken: attempt.accessToken };
      });
    } finally {
      // Cleared SYNCHRONOUSLY, and the window it leaves is deliberately not
      // load-bearing: a 401 answered after this point is handled by
      // authedFetch's sent-token comparison (§4.3 branch 3), which is correct
      // for ANY delay. Do not "fix" this by deferring it to a macrotask and
      // conclude the race is thereby closed — it only widens the window by one
      // turn of the event loop, while the 401 it must survive can arrive
      // seconds later.
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
