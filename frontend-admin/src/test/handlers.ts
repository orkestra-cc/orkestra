import { http, HttpResponse } from 'msw';

// Wildcard host so handlers match regardless of how baseApi resolves
// VITE_BACKEND_URL (e.g. console.localhost:3000 in dev, anything in tests).
export const url = (path: string) => `*${path}`;

// Per-test captured outbound request state — handlers stash request
// params here so tests can assert what the SPA sent. Reset between tests
// via resetCapturedRequests() in setup.ts. Empty in the core-only base
// (ADR-0006); a fork's handlers repopulate it as needed.
export const capturedRequests: Record<string, URLSearchParams | null> = {};

export const resetCapturedRequests = () => {
  for (const k of Object.keys(capturedRequests)) capturedRequests[k] = null;
};

// --- Self-service security center (/user/security) ---

// Default empty self-auth-methods. Tests that need a populated state
// pass an override to selfAuthMethodsHandler.
export const emptySelfAuthMethods = {
  hasUsablePassword: true,
  emailVerified: true,
  mfaRequired: false,
  mfaFactors: [] as Array<{
    type: 'totp' | 'webauthn';
    enrolledAt?: string;
    lastUsedAt?: string;
    backupCodesRemaining?: number;
  }>,
  oauthProviders: [] as Array<{
    provider: 'google' | 'apple' | 'github' | 'discord';
    email: string;
    linkedAt: string;
    isPrimary: boolean;
  }>
};

export const selfAuthMethodsHandler = (
  body: typeof emptySelfAuthMethods = emptySelfAuthMethods
) =>
  http.get(url('/v1/auth/operator/me/auth-methods'), () =>
    HttpResponse.json(body)
  );

export const emptySessions = {
  sessions: [] as Array<{
    sessionId: string;
    deviceId: string;
    deviceName: string;
    deviceType: string;
    platform: string;
    ipAddress: string;
    lastActivity: string;
    createdAt: string;
    expiresAt: string;
    isCurrent: boolean;
  }>,
  activeCount: 0
};

export const mySessionsHandler = (body: typeof emptySessions = emptySessions) =>
  http.get(url('/v1/auth/operator/me/sessions'), () => HttpResponse.json(body));

export const emptyTrustedDevices = {
  devices: [] as Array<{
    uuid: string;
    deviceId: string;
    deviceName: string;
    platform: string;
    trustedAt: string;
    trustedUntil: string;
  }>
};

export const trustedDevicesHandler = (
  body: typeof emptyTrustedDevices = emptyTrustedDevices
) =>
  http.get(url('/v1/auth/operator/me/devices/trust'), () =>
    HttpResponse.json(body)
  );

// Default handlers used by every test unless overridden. Keep this list
// small — only stub endpoints the harness itself depends on, plus any
// chatty endpoints that components fire on mount (none yet).
export const defaultHandlers = [];
