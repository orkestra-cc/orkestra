import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import { http, HttpResponse } from 'msw';
import { server } from 'test/server';
import { setupStore } from 'test/render';
import { DEFAULT_POST_LOGIN } from 'utils/returnTo';
import { baseApi, setNavigateToLogin } from './baseApi';
import { mfaApi } from './mfaApi';
import { setupApi } from './setupApi';
import { completeStepUp, subscribeStepUp } from '../stepUp';

// The toast is mocked for the same reason baseApi.sessionEnded.test.ts mocks
// it: react-toastify renders into a container this suite never mounts, and
// "was the operator told anything" has to be observable. This branch is
// deliberately silent — like its two sibling gate answers, and unlike the
// session-ended codes — so the spy is here to prove the silence.
const toastError = vi.hoisted(() => vi.fn());
vi.mock('react-toastify', () => ({
  toast: { error: toastError, success: vi.fn(), info: vi.fn(), warn: vi.fn() }
}));

const ENROLL_BEGIN = '*/v1/auth/operator/mfa/enroll/begin';

// Answers the real enrolment endpoint with a coded 401 and counts any
// silent-refresh attempt. This branch must not rotate: the session is alive,
// it is merely too old to add a factor, and a token minted from the same
// cookie carries the same auth_time (backend middleware/auth.go), so a
// rotation could not change the answer.
const respondWith = (body: Record<string, unknown>) => {
  let refreshAttempts = 0;
  server.use(
    http.post(ENROLL_BEGIN, () => HttpResponse.json(body, { status: 401 })),
    http.post('*/refresh*', () => {
      refreshAttempts += 1;
      return HttpResponse.json({}, { status: 200 });
    })
  );
  return () => refreshAttempts;
};

// Mirrors baseApi.sessionEnded.test.ts: a seeded access token so "cleared"
// is not vacuously true, and setupCompleted so the first-install gate is not
// what returns early. The gate sits AFTER the branch under test, but seeding
// it keeps the test honest if the order is ever changed.
const setupSeededStore = async () => {
  const store = setupStore({
    auth: {
      user: null,
      isAuthenticated: true,
      isLoading: false,
      error: null,
      sessionExpiry: null,
      permissions: [],
      preferences: { theme: 'light', language: 'en', notifications: true },
      _isLoggingOut: false,
      accessToken: 'seed-access-token',
      tokenExpiry: new Date(Date.now() + 60_000).toISOString()
    }
  });
  await store.dispatch(
    setupApi.util.upsertQueryData('getSetupStatus', undefined, {
      setupCompleted: true,
      phase: 'complete',
      smtpConfigured: true
    })
  );
  return store;
};

const navigate = vi.fn();

beforeEach(() => {
  toastError.mockClear();
  navigate.mockClear();
  setNavigateToLogin(navigate);
  window.history.pushState({}, '', '/');
  setupStore().dispatch(baseApi.util.resetApiState());
});

afterEach(() => {
  // The seam is module-level state on baseApi; AuthProvider resets it to a
  // no-op on unmount and so must this suite, or the next file's 401s call a
  // spy that belongs to a finished test.
  setNavigateToLogin(() => {});
});

describe('reauthentication_required drives a re-login, not a modal', () => {
  // The console already routes step_up_required to StepUpModal and replays.
  // reauthentication_required has no modal answer — a step-up needs a factor
  // the caller does not have, and a password reconfirm is wrong for an
  // OAuth-only account and refused for an MFA-obligated one inside its grace
  // window — so the answer is a fresh sign-in, uniform for every population.
  it('clears the session and sends the operator to login with the current path', async () => {
    window.history.pushState({}, '', '/user/security?tab=mfa');
    const attempts = respondWith({
      status: 401,
      code: 'reauthentication_required',
      maxAgeSeconds: 300,
      authTime: 0
    });
    const store = await setupSeededStore();

    await store.dispatch(mfaApi.endpoints.enrollMfaBegin.initiate());

    expect(navigate).toHaveBeenCalledWith('/user/security?tab=mfa');
    expect(store.getState().auth.accessToken).toBeFalsy();
    // No rotation: the refusal is about the session's AGE, and a token minted
    // from the same refresh cookie carries the same auth_time.
    expect(attempts()).toBe(0);
    // Silent, like the other two gate answers. The login form is the message.
    expect(toastError).not.toHaveBeenCalled();
  });

  // An open redirect here would be handed an attacker-controlled destination
  // on every stale enrolment attempt. window.location is influenceable within
  // the origin — history.pushState keeps the origin but not the shape.
  //
  // The encoded-slash leader stands in for the bare "//evil.example.com"
  // case, which happy-dom's pushState refuses outright as a cross-origin
  // state object; "/login" is the other rejection sanitizeReturnTo makes,
  // and a redirect that bounced back into the login route would loop.
  it.each([
    ['an encoded protocol-relative leader', '/%2fevil.example.com/steal'],
    ['the login route itself', '/login']
  ])('sanitises %s down to the default landing', async (_label, path) => {
    window.history.pushState({}, '', path);
    respondWith({ code: 'reauthentication_required' });
    const store = await setupSeededStore();

    await store.dispatch(mfaApi.endpoints.enrollMfaBegin.initiate());

    // DEFAULT_POST_LOGIN, not `undefined`: AuthProvider fills an absent
    // argument back in from location.pathname, which is the rejected value.
    expect(navigate).toHaveBeenCalledWith(DEFAULT_POST_LOGIN);
    expect(navigate).not.toHaveBeenCalledWith(path);
  });

  // The pre-existing step-up path is untouched: modal and replay, never a
  // redirect. An enrolled user replacing a factor still gets this code.
  it('leaves step_up_required on the modal path', async () => {
    const opened = vi.fn();
    // The modal is not mounted here, so nothing would ever resolve the
    // pending promise and the dispatch below would hang. Stand in for it.
    const unsubscribe = subscribeStepUp(open => {
      if (!open) return;
      opened();
      completeStepUp(false);
    });
    respondWith({ code: 'step_up_required' });
    const store = await setupSeededStore();

    await store.dispatch(mfaApi.endpoints.enrollMfaBegin.initiate());
    unsubscribe();

    expect(opened).toHaveBeenCalledTimes(1);
    expect(navigate).not.toHaveBeenCalled();
    expect(store.getState().auth.accessToken).toBe('seed-access-token');
  });
});

// The return path is one answer, not four. Three of these call sites passed
// window.location.pathname alone, which was invisible while AuthProvider
// stored `state.from` as a string and locationToReturnTo dropped it — every
// interceptor redirect landed on DEFAULT_POST_LOGIN regardless. Making the
// deep link survive is what made the omission user-visible: a revoked session
// would return to /admin/modules having silently lost ?tab=addons.
describe('every interceptor redirect carries the same current path', () => {
  it.each([
    ['session_revoked', 'session_revoked'],
    ['session_max_age_reached', 'session_max_age_reached'],
    ['reauthentication_required', 'reauthentication_required']
  ])('keeps the query string on %s', async (_label, code) => {
    window.history.pushState({}, '', '/admin/modules?tab=addons');
    respondWith({ code });
    const store = await setupSeededStore();

    await store.dispatch(mfaApi.endpoints.enrollMfaBegin.initiate());

    expect(navigate).toHaveBeenCalledWith('/admin/modules?tab=addons');
  });
});
