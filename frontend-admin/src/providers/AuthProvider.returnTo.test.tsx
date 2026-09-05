import { describe, it, expect, beforeEach, vi } from 'vitest';
import { screen, waitFor } from '@testing-library/react';
import { http, HttpResponse } from 'msw';
import { useLocation } from 'react-router';
import { server } from 'test/server';
import { renderWithProviders } from 'test/render';
import { locationToReturnTo, sanitizeReturnTo } from 'utils/returnTo';
import { baseApi } from 'store/api/baseApi';
import { mfaApi } from 'store/api/mfaApi';
import { setupApi } from 'store/api/setupApi';
import AuthProvider from './AuthProvider';

vi.mock('react-toastify', () => ({
  toast: { error: vi.fn(), success: vi.fn(), info: vi.fn(), warn: vi.fn() }
}));

// Reports where the router went and what it was handed. `state.from` is the
// console's ONE return-path convention (there is no ?next= anywhere in this
// SPA), so the round trip has to be asserted on the state, not on a URL.
const LocationProbe = () => {
  const location = useLocation();
  return (
    <>
      <span data-testid="path">{location.pathname}</span>
      <span data-testid="from">
        {JSON.stringify(
          (location.state as { from?: unknown } | null)?.from ?? null
        )}
      </span>
    </>
  );
};

beforeEach(() => {
  server.use(
    // AuthProvider mounts useAuth() → useGetSessionQuery. Unstubbed it would
    // fail the whole file: src/test/setup.ts runs MSW with
    // onUnhandledRequest: 'error'.
    http.get('*/v1/auth/session', () => HttpResponse.json({ user: null })),
    http.post('*/v1/auth/operator/mfa/enroll/begin', () =>
      HttpResponse.json(
        { code: 'reauthentication_required', maxAgeSeconds: 300, authTime: 0 },
        { status: 401 }
      )
    ),
    http.post('*/refresh*', () => HttpResponse.json({}, { status: 200 }))
  );
  window.history.pushState({}, '', '/');
});

describe('AuthProvider — the interceptor redirect keeps its deep link', () => {
  // The whole point of putting a path on the redirect. Before this the
  // callback stored a STRING, locationToReturnTo rejects strings by design
  // (returnTo.ts, pinned in returnTo.test.ts), and so every
  // interceptor-driven redirect quietly lost its destination — while
  // ProtectedRoute, the other producer of state.from, stored a Location.
  // Two shapes, one reader: one of them could never work.
  it('hands the login form a Location-shaped `from` the reader accepts', async () => {
    window.history.pushState({}, '', '/user/security?tab=mfa');
    const { store } = renderWithProviders(
      <AuthProvider>
        <LocationProbe />
      </AuthProvider>
    );
    await store.dispatch(
      setupApi.util.upsertQueryData('getSetupStatus', undefined, {
        setupCompleted: true,
        phase: 'complete',
        smtpConfigured: true
      })
    );

    await store.dispatch(mfaApi.endpoints.enrollMfaBegin.initiate());

    await waitFor(() =>
      expect(screen.getByTestId('path')).toHaveTextContent('/login')
    );
    const from = JSON.parse(screen.getByTestId('from').textContent ?? 'null');
    // The assertion that matters is not the literal shape but that the
    // console's own reader gets the deep link back out of it.
    expect(sanitizeReturnTo(locationToReturnTo(from))).toBe(
      '/user/security?tab=mfa'
    );
    store.dispatch(baseApi.util.resetApiState());
  });
});
