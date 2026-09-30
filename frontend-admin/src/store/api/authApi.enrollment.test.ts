import { describe, it, expect } from 'vitest';
import { waitFor } from '@testing-library/react';
import { http, HttpResponse } from 'msw';
import { server } from 'test/server';
import { setupStore } from 'test/render';
import { emptySelfAuthMethods, url } from 'test/handlers';
import { authApi } from './authApi';
import { deviceTrustApi } from './deviceTrustApi';

describe('initial-password credential cache updates', () => {
  it.each([200, 409])(
    'refreshes session and trust caches only after successful creation (%s)',
    async status => {
      let created = false;
      let sessionReads = 0;
      let trustReads = 0;
      server.use(
        http.get(url('/v1/auth/operator/me/auth-methods'), () =>
          HttpResponse.json(emptySelfAuthMethods)
        ),
        http.get(url('/v1/auth/operator/me/sessions'), () => {
          sessionReads++;
          return HttpResponse.json({
            sessions: [],
            activeCount: created ? 1 : 3
          });
        }),
        http.get(url('/v1/auth/operator/me/devices/trust'), () => {
          trustReads++;
          return HttpResponse.json({
            devices: created
              ? []
              : [
                  {
                    uuid: 'trust-old',
                    deviceId: 'other',
                    trustedAt: '2026-09-01T00:00:00Z',
                    trustedUntil: '2026-10-01T00:00:00Z'
                  }
                ]
          });
        }),
        http.post(url('/v1/auth/operator/me/password'), () => {
          created = status === 200;
          return HttpResponse.json(
            status === 200
              ? { message: 'Password added' }
              : { code: 'auth.password_already_set' },
            { status }
          );
        })
      );
      const store = setupStore();
      const sessions = store.dispatch(
        authApi.endpoints.getMySessions.initiate()
      );
      const trust = store.dispatch(
        deviceTrustApi.endpoints.listTrustedDevices.initiate()
      );
      const methods = store.dispatch(
        authApi.endpoints.getSelfAuthMethods.initiate()
      );
      try {
        await Promise.all([
          sessions.unwrap(),
          trust.unwrap(),
          methods.unwrap()
        ]);
        await store.dispatch(
          authApi.endpoints.setInitialPassword.initiate({
            newPassword: 'Correct-Horse-42!'
          })
        );
        if (status === 200) {
          await waitFor(() => {
            expect(
              authApi.endpoints.getMySessions.select()(store.getState()).data
                ?.activeCount
            ).toBe(1);
            expect(
              deviceTrustApi.endpoints.listTrustedDevices.select()(
                store.getState()
              ).data?.devices
            ).toEqual([]);
          });
          expect(sessionReads).toBe(2);
          expect(trustReads).toBe(2);
        } else {
          expect(
            authApi.endpoints.getMySessions.select()(store.getState()).status
          ).toBe('fulfilled');
          expect(
            deviceTrustApi.endpoints.listTrustedDevices.select()(
              store.getState()
            ).status
          ).toBe('fulfilled');
          expect(sessionReads).toBe(1);
          expect(trustReads).toBe(1);
          expect(
            authApi.endpoints.getMySessions.select()(store.getState()).data
              ?.activeCount
          ).toBe(3);
          expect(
            deviceTrustApi.endpoints.listTrustedDevices.select()(
              store.getState()
            ).data?.devices
          ).toHaveLength(1);
        }
      } finally {
        sessions.unsubscribe();
        trust.unsubscribe();
        methods.unsubscribe();
      }
    }
  );
});
