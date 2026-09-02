// The API base-URL resolver, and nothing else.
//
// This file used to export a generated-types OpenAPI client `api` with a bearer
// middleware and a 401 refresh-and-retry middleware. Nothing ever imported it,
// and it was WRONG in five ways at once — it retried every 401 including a
// wrong-password change-password, re-sent an already-consumed request body,
// used the marker-gated refresh, cleared the token but not the session marker,
// and inspected no error code. It read as the live request path (the docs said
// so until 82f25252) while nothing routed through it, which is the trap #325
// was born from, so it was deleted rather than left dormant.
//
// The live authenticated path is src/api/authedFetch.ts, and it is the ONLY
// 401 algorithm in this tree. The generated-types stub that used to sit beside
// this file, the typed-client runtime that would have consumed it and the
// codegen script that produced it all left with it (§8 #4): nothing imported
// any of them, so what they kept warm was not a typed client but the MATERIALS
// for a second 401 algorithm. If one is ever wanted, it re-adds a pinned
// dependency in the same PR that writes the middleware — and that middleware
// must DELEGATE to authedFetch's policy rather than restate it.

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
