import '@testing-library/jest-dom/vitest';
import { afterAll, afterEach, beforeAll } from 'vitest';
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
