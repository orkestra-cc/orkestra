// The ONE authenticated request path, and the ONLY 401 algorithm in the tree.
//
// Before this, four near-copies of "attach bearer + credentials:'include'"
// existed (auth.ts::authedFetch, avatar.ts::authedJson,
// billingProfile.ts::authedJson, dsr.ts::postJson) plus a fifth, unreachable
// and unsafe one in client.ts. After it there is one, a new endpoint cannot
// forget the recovery, and there is no second implementation to drift from it
// or to be picked up by mistake.
//
// The refresh endpoint is called through tokenStore, never through this
// helper, so recursion is structurally impossible.
//
// A streaming body is unsupported: the retry re-sends from `init`, and a
// stream cannot be replayed. No call site has one.
import { apiBaseURL } from "@/api/client";
import {
  clearSessionLocally,
  getAccessTokenSnapshot,
  refreshAccessToken,
  refreshAfterUnauthorized,
} from "@/auth/tokenStore";

// The CLOSED set of 401 codes that mean the session itself is over. It is a
// MEMBERSHIP test and deliberately not a presence test: the middleware emits at
// least seven distinct top-level codes and four of them ride on 401s that are
// emphatically not a dead session (step_up_required, mfa_enrollment_required,
// password_confirm_required, audience_mismatch). `if (body.code) → clear` reads
// as the obvious simplification and would sign a user out for being asked to
// confirm a password. Adding to this set is a decision, not a chore.
export const TERMINAL_CODES = new Set([
  "session_revoked",
  "session_max_age_reached",
]);

// The server's own statement that it rejected the bearer BEFORE dispatch
// (RequireAuth, §3.D). It is proof the request never reached its handler, so
// it is safe to refresh and retry — including for a token that expired in
// flight, which our own reckoning cannot cover. It means the OPPOSITE of
// terminal, so it is never a member of the set above.
export const CODE_ACCESS_TOKEN_EXPIRED = "access_token_expired";

// Reads a CLONE, never the response a caller will get. A body that is absent,
// not JSON, or carries no top-level `code` simply yields null — the ordinary
// case, not an error condition: the generic paths emit no top-level code,
// keeping their internal one in errors[0].value, which we deliberately do not
// read (it is CodeInvalidCredentials for an AuthenticationError, the same value
// a wrong password produces).
//
// The WWW-Authenticate shortcut is NOT available: that header is not in the
// API's CORS ExposedHeaders (cmd/server/middleware.go:103) and this SPA is
// cross-origin to the API host, so JS cannot read it. Do not "simplify" the
// clone away by reaching for it without adding the header to that list first.
async function read401Code(clone: Response): Promise<string | null> {
  const body = (await clone.json().catch(() => ({}))) as { code?: unknown };
  return typeof body.code === "string" ? body.code : null;
}

const isJsonBody = (body: BodyInit | null | undefined): boolean =>
  typeof body === "string";

function doFetch(
  path: string,
  init: RequestInit | undefined,
  token: string | null,
): Promise<Response> {
  // new Headers, NOT object spread. `init.headers` is a HeadersInit: a plain
  // object, a Headers instance, or an array of tuples. Spreading a Headers
  // yields {} — it has no own enumerable properties — so every header the
  // caller set is dropped SILENTLY; spreading a tuple array yields
  // {0: [...], 1: [...]}, which fetch then rejects or mangles.
  const headers = new Headers(init?.headers);
  if (!headers.has("Accept")) headers.set("Accept", "application/json");
  // Only for a body we KNOW is JSON. Forcing application/json on FormData
  // destroys the multipart boundary.
  if (!headers.has("Content-Type") && isJsonBody(init?.body)) {
    headers.set("Content-Type", "application/json");
  }
  // Last, via `set`: not appended, not conditional on absence. This is where
  // the precedence decision is enforced — a call site cannot override the
  // bearer, whatever shape it passed its headers in.
  if (token) headers.set("Authorization", `Bearer ${token}`);
  return fetch(`${apiBaseURL}${path}`, {
    ...init,
    // After the spread, so it always wins: the httpOnly refresh cookie is
    // Domain-scoped to the API host (ADR-0003 D-9) and only attaches when
    // credentials are explicitly included.
    credentials: "include",
    headers,
  });
}

// At most ONE retry per call, whichever branch produced it. The retry state is
// a local variable, not an X-Retry header (§5.10): a header would travel to the
// server and back, and nothing outside this function needs to know. The retry's
// own 401 is inspected too — but only for the terminal set. A codeless 401
// there stays ambiguous (it can be the endpoint's own answer, the §4.4 mirror
// case being one), and clearing on it would sign out a user whose session is
// fine because they mistyped a password. `access_token_expired` on a retry is
// deliberately NOT acted on: a second refresh is forbidden regardless.
async function retryOnce(
  path: string,
  init: RequestInit | undefined,
  token: string,
): Promise<Response> {
  const retried = await doFetch(path, init, token);
  if (retried.status === 401) {
    const code = await read401Code(retried.clone());
    if (code !== null && TERMINAL_CODES.has(code)) {
      // The session died between the refresh and the retry. Leaving a token
      // the server rejects is defect A's broken state all over again (G3).
      clearSessionLocally();
    }
  }
  return retried;
}

export async function authedFetch(
  path: string,
  init?: RequestInit,
): Promise<Response> {
  // Captured together, BEFORE the fetch: at 401 time the store's expiry may
  // already belong to a token a sibling installed, and `sentAt` is the instant
  // the whole decision below turns on.
  const sent = getAccessTokenSnapshot();
  const sentAt = Date.now();
  const res = await doFetch(path, init, sent.token);
  if (res.status !== 401) return res;

  // A Response body is a single-use stream. Every inspection reads a CLONE, so
  // whatever we hand back is still unread — the caller's readError does
  // `await res.json()` inside a `.catch(() => ({}))`, so reading it here would
  // degrade SILENTLY into "fallback message, no code".
  const code = await read401Code(res.clone());

  // 1. Terminal: a token minted from the same cookie carries the same dead
  //    sid, so there is nothing to recover. No refresh, no retry.
  if (code !== null && TERMINAL_CODES.has(code)) {
    clearSessionLocally();
    return res;
  }

  // 2. THE REPLAY GUARD, and it sits ahead of every recovery branch.
  //
  //    Recovery is permitted only on PROOF that the request never reached its
  //    handler — otherwise a retry re-sends whatever it consumed. The
  //    motivating case: change-password answers 401 when the CURRENT PASSWORD
  //    IN THE BODY is wrong, and a replayed attempt is counted again, so two
  //    mistypes trip the lockout as though there had been four.
  //
  //    Two independent proofs, either sufficient:
  //      (a) the server says it rejected the bearer before dispatch (§3.D);
  //      (b) the token was ALREADY EXPIRED when it left — RequireAuth accepts
  //          a token until the instant it expires, with no grace, so this is
  //          the weakest client-side condition that still proves it.
  //    (b) is the fallback for a backend that has not shipped (a) yet.
  //
  //    NO MARGIN. A `SKEW` here is precisely the round-11 hole: a token with
  //    20s of life is still accepted by the server, so the handler DID run.
  //    An UNKNOWN expiry counts as live for the same reason — it cannot prove
  //    the handler did not run.
  const serverSaysExpired = code === CODE_ACCESS_TOKEN_EXPIRED;
  const provablyExpiredAtSend =
    sent.expiresAt !== null && sent.expiresAt <= sentAt;
  if (!serverSaysExpired && !provablyExpiredAtSend) return res;

  // 3. A sibling already rotated: prefer its token over a second rotation
  //    (G8). Safe by the same proof as branches 4a/4b, because this sits
  //    behind the guard above.
  const current = getAccessTokenSnapshot();
  if (current.token !== null && current.token !== sent.token) {
    return retryOnce(path, init, current.token);
  }

  // 4a/4b. Split on "did we send a bearer?", which is the honest question. A
  //    bearer in memory is proof a session existed, so refreshAccessToken's
  //    marker gate — which answers `signed-out` while clearing NOTHING — must
  //    not get to veto the cookie. A true anonymous visitor keeps the
  //    optimisation the gate was written for.
  const outcome =
    sent.token !== null
      ? await refreshAfterUnauthorized(apiBaseURL)
      : await refreshAccessToken(apiBaseURL);
  // `signed-out`: performRefresh has already cleared token AND marker (G3).
  // `unavailable`: token and marker untouched (G2, G7). Either way the caller
  // gets the original response, unread.
  if (outcome.status !== "ok") return res;
  return retryOnce(path, init, outcome.accessToken);
}
