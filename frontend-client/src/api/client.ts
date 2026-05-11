import createClient, { type Middleware } from 'openapi-fetch';

import type { paths } from '@/api/openapi.gen';
import { getAccessToken, refreshAccessToken, clearAccessToken } from '@/auth/tokenStore';

const API_BASE =
  import.meta.env.VITE_API_BASE?.replace(/\/$/, '') ?? 'http://api.localhost:3000';

// Bearer middleware — pulls the in-memory access token on every request.
// The token store is module-scoped so React strict-mode double-mounts
// don't mint a second copy.
const authMiddleware: Middleware = {
  onRequest({ request }) {
    const token = getAccessToken();
    if (token) {
      request.headers.set('Authorization', `Bearer ${token}`);
    }
    return request;
  },
};

// 401 silent-refresh middleware. When a request comes back 401 with the
// access token expired, hit /v1/auth/client/refresh-cookie (which uses
// the httpOnly refresh cookie set at login by the backend per ADR-0003
// PR-D D-9) and retry the original request once. Two consecutive 401s
// trigger logout — the SPA's auth context routes the user back to /login.
const refreshMiddleware: Middleware = {
  async onResponse({ request, response }) {
    if (response.status !== 401 || request.headers.get('X-Retry') === '1') {
      return response;
    }
    const fresh = await refreshAccessToken(API_BASE);
    if (!fresh) {
      clearAccessToken();
      return response;
    }
    const retried = await fetch(request.url, {
      method: request.method,
      headers: (() => {
        const h = new Headers(request.headers);
        h.set('Authorization', `Bearer ${fresh}`);
        h.set('X-Retry', '1');
        return h;
      })(),
      body: request.body,
      credentials: 'include',
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
  credentials: 'include',
});

api.use(authMiddleware);
api.use(refreshMiddleware);

export const apiBaseURL = API_BASE;
