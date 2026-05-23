import '@testing-library/jest-dom/vitest';
import { afterAll, afterEach, beforeAll } from 'vitest';
// Initialize i18n once for the whole test suite so t('...') calls
// resolve against the real en.json bundle instead of returning raw
// keys — the existing EmailPasswordForm tests assert on rendered
// English strings.
import '../i18n';
import { server } from './server';
import { resetCapturedRequests } from './handlers';

// Throw on any unhandled request so tests can't silently pass against a
// missing stub. Add the endpoint to defaultHandlers (or override per-test
// via server.use(...)) when this fires.
beforeAll(() => server.listen({ onUnhandledRequest: 'error' }));

afterEach(() => {
  server.resetHandlers();
  resetCapturedRequests();
});

afterAll(() => server.close());
