// The API base-URL resolver, and nothing else.
//
// This file used to export an openapi-fetch client `api` with a bearer
// middleware and a 401 refresh-and-retry middleware. Nothing ever imported it,
// and it was WRONG in five ways at once — it retried every 401 including a
// wrong-password change-password, re-sent an already-consumed request body,
// used the marker-gated refresh, cleared the token but not the session marker,
// and inspected no error code. It read as the live request path (the docs said
// so until 82f25252) while nothing routed through it, which is the trap #325
// was born from, so it was deleted rather than left dormant.
//
// The live authenticated path is src/api/authedFetch.ts, and it is the ONLY
// 401 algorithm in this tree. openapi.gen.ts and the openapi-fetch dependency
// both stay — the codegen target the README documents, and what a future typed
// client will be built on; that client must DELEGATE to authedFetch's policy
// rather than restate it.

// Runtime config — see frontend-client/public/config.js + Dockerfile entrypoint.
// VITE_API_BASE is consulted only as a build-time fallback for envs that don't
// set window.__ORKESTRA_CONFIG__ (Vitest, scratch SSR, etc.).
declare global {
  interface Window {
    __ORKESTRA_CONFIG__?: { apiBase?: string; stripePublishableKey?: string };
  }
}
const runtimeApiBase =
  typeof window !== "undefined"
    ? window.__ORKESTRA_CONFIG__?.apiBase
    : undefined;
const API_BASE = (
  runtimeApiBase ??
  import.meta.env.VITE_API_BASE ??
  "http://client.localhost:3000"
).replace(/\/$/, "");

export const apiBaseURL = API_BASE;
