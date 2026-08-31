import createClient, { type Middleware } from "openapi-fetch";

import type { paths } from "@/api/openapi.gen";
import {
  getAccessToken,
  refreshAccessToken,
  clearAccessToken,
} from "@/auth/tokenStore";

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
  "http://api.localhost:3000"
).replace(/\/$/, "");

// Bearer middleware — pulls the in-memory access token on every request.
// The token store is module-scoped so React strict-mode double-mounts
// don't mint a second copy.
const authMiddleware: Middleware = {
  onRequest({ request }) {
    const token = getAccessToken();
    if (token) {
      request.headers.set("Authorization", `Bearer ${token}`);
    }
    return request;
  },
};

// 401 silent-refresh middleware. When a request comes back 401 with the
// access token expired, hit /v1/auth/client/refresh-cookie (which uses
// the httpOnly refresh cookie set at login by the backend per ADR-0003
// PR-D D-9) and retry the original request once. Two consecutive 401s
// trigger logout — the SPA's auth context routes the user back to /login.
//
// A refresh that comes back `unavailable` is the exception: a 503 means
// the server could not evaluate the session, and a transport failure (the
// fetch itself rejecting) means nothing is known about it — neither is the
// same as the session being over. Surfacing the original 401 without
// clearing the token leaves the user signed in so the next request can
// retry. See RefreshOutcome and performRefresh in tokenStore.ts and
// ADR-0017.
const refreshMiddleware: Middleware = {
  async onResponse({ request, response }) {
    if (response.status !== 401 || request.headers.get("X-Retry") === "1") {
      return response;
    }
    const outcome = await refreshAccessToken(API_BASE);
    if (outcome.status === "unavailable") {
      return response;
    }
    if (outcome.status !== "ok") {
      clearAccessToken();
      return response;
    }
    const retried = await fetch(request.url, {
      method: request.method,
      headers: (() => {
        const h = new Headers(request.headers);
        h.set("Authorization", `Bearer ${outcome.accessToken}`);
        h.set("X-Retry", "1");
        return h;
      })(),
      body: request.body,
      credentials: "include",
    });
    return retried;
  },
};

// `credentials: 'include'` is critical — the refresh cookie is httpOnly
// and Domain-scoped to api.localhost (dev) / app.orkestra.cc (staging)
// per ADR-0003 D-9, so the browser will only attach it when same-site
// cookies are explicitly enabled on every request to the API origin.
export const api = createClient<paths>({
  baseUrl: API_BASE,
  credentials: "include",
});

api.use(authMiddleware);
api.use(refreshMiddleware);

export const apiBaseURL = API_BASE;
