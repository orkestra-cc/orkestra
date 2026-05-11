import { http, HttpResponse } from 'msw';
import type { BillingStats } from 'types/billing';

// Wildcard host so handlers match regardless of how baseApi resolves
// VITE_BACKEND_URL (e.g. console.localhost:3000 in dev, anything in tests).
export const url = (path: string) => `*${path}`;

// Default empty BillingStats — every counter at zero. Most tests can use
// this as-is; tests that need a populated response override it via
// server.use(billingStatsHandler({...})).
export const emptyBillingStats: BillingStats = {
  issuedTotal: 0,
  issuedDraft: 0,
  issuedSent: 0,
  issuedDelivered: 0,
  issuedRejected: 0,
  issuedAmount: 0,
  receivedTotal: 0,
  receivedPending: 0,
  receivedAccepted: 0,
  receivedRejected: 0,
  receivedAmount: 0,
  unprocessedNotifications: 0,
  pendingActions: 0,
  weeklyData: [],
  periodStart: '2000-01-01T00:00:00Z',
  periodEnd: '2099-12-31T23:59:59Z',
};

// Captured params from the most recent /v1/billing/stats request — used by
// regression tests that need to assert what fromDate/toDate the SPA sent.
// Reset between tests via resetCapturedRequests() in setup.ts.
export const capturedRequests = {
  billingStatsParams: null as URLSearchParams | null,
};

export const resetCapturedRequests = () => {
  capturedRequests.billingStatsParams = null;
};

export const billingStatsHandler = (
  body: BillingStats = emptyBillingStats,
) =>
  http.get(url('/v1/billing/stats'), ({ request }) => {
    capturedRequests.billingStatsParams = new URL(request.url).searchParams;
    return HttpResponse.json(body);
  });

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
  }>,
};

export const selfAuthMethodsHandler = (
  body: typeof emptySelfAuthMethods = emptySelfAuthMethods,
) =>
  http.get(url('/v1/auth/operator/me/auth-methods'), () =>
    HttpResponse.json(body),
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
  activeCount: 0,
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
  }>,
};

export const trustedDevicesHandler = (
  body: typeof emptyTrustedDevices = emptyTrustedDevices,
) =>
  http.get(url('/v1/auth/operator/me/devices/trust'), () =>
    HttpResponse.json(body),
  );

// Default handlers used by every test unless overridden. Keep this list
// small — only stub endpoints the harness itself depends on, plus any
// chatty endpoints that components fire on mount (none yet).
export const defaultHandlers = [billingStatsHandler()];
