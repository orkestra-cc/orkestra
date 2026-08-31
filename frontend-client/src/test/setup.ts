// MUST stay the first import: it restores localStorage/sessionStorage on
// Node >= 25 before anything else can touch them. See webStorage.ts.
import "./webStorage";
import "@testing-library/jest-dom/vitest";
import { cleanup } from "@testing-library/react";
import { afterAll, afterEach, beforeAll, vi } from "vitest";

import i18n from "@/i18n";
import { clearAccessToken } from "@/auth/tokenStore";
import { server } from "./server";

beforeAll(async () => {
  // Deterministic copy: the language detector would otherwise pick
  // whatever happy-dom reports for navigator.language. Tests assert the
  // English strings of src/locales/en.json.
  await i18n.changeLanguage("en");
  // Throw on any unhandled request so a test cannot pass against a
  // missing stub. Add the endpoint to defaultHandlers or override
  // per-test via server.use(...) when this fires.
  server.listen({ onUnhandledRequest: "error" });
});

afterEach(() => {
  // First: a test that stubbed a global (tokenStore.test.ts installs a
  // throwing localStorage) must not leak it into the storage reset below.
  vi.unstubAllGlobals();
  // React Testing Library registers its automatic cleanup only when a
  // global afterEach exists (globals: false here) — unmount explicitly, or
  // the next test finds the previous tree still mounted and its
  // AuthProvider still subscribed to the token store.
  cleanup();
  server.resetHandlers();
  // tokenStore is module-scoped state; a token left by one test would
  // make the next render start authenticated.
  clearAccessToken();
  localStorage.clear();
  sessionStorage.clear();
});

afterAll(() => server.close());
