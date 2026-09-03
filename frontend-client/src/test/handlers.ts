import { http, HttpResponse, type RequestHandler } from "msw";

import type { AuthPolicy } from "@/api/auth";

// Wildcard host so handlers match whatever apiBaseURL resolves to
// (window.__ORKESTRA_CONFIG__, VITE_API_BASE, or the built-in default).
export const url = (path: string) => `*${path}`;

// The client /policy with everything enabled. Per-test overrides flip the
// PR 3 password-login field; passwordLoginBreakGlassEffective is always
// false on this tier (spec §4.9).
export const openPolicy: AuthPolicy = {
  registrationEnabled: true,
  loginEnabled: true,
  passwordMinLength: 10,
  passwordLoginEnabled: true,
  passwordLoginBreakGlassEffective: false,
};

export const clientPolicyHandler = (overrides: Partial<AuthPolicy> = {}) =>
  http.get(url("/v1/auth/client/policy"), () =>
    HttpResponse.json({ ...openPolicy, ...overrides }),
  );

// GET /v1/auth/client/providers → {providers: string[]} (auth_handler.go:409).
export const providersHandler = (providers: string[]) =>
  http.get(url("/v1/auth/client/providers"), () =>
    HttpResponse.json({ providers }),
  );

// The document-level failure: 503 auth.policy_unavailable (auth_handler.go:424).
export const providersUnavailableHandler = () =>
  http.get(url("/v1/auth/client/providers"), () =>
    HttpResponse.json(
      {
        code: "auth.policy_unavailable",
        detail: "Sign-in policy is temporarily unavailable; try again shortly",
      },
      { status: 503 },
    ),
  );

// Default handlers used by every test unless overridden. Keep this list
// empty: a component that mounts an endpoint must stub it explicitly, so a
// missing stub is a red run, never a silently passing one.
export const defaultHandlers: RequestHandler[] = [];

// GET /v1/auth/client/me — the minimum MeResponse Layout (header avatar +
// nav) and AccountPage read once a session exists.
export const meHandler = (overrides: Record<string, unknown> = {}) =>
  http.get(url("/v1/auth/client/me"), () =>
    HttpResponse.json({
      id: "u-1",
      email: "user@example.com",
      fullName: "Client User",
      emailVerified: true,
      isActive: true,
      ...overrides,
    }),
  );
