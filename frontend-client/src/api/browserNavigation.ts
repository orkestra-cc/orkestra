// browserNavigation is the one seam through which the SPA leaves the router —
// a plain object so tests can spy on `assign` without touching window.location
// (not configurable under happy-dom / jsdom).
//
// It lives in its OWN module, and that module imports nothing. Two callers now
// need the seam and they sit on opposite sides of an existing import edge:
// api/auth.ts already imports api/authedFetch.ts, so leaving the seam in
// auth.ts and reaching for it from the fetch wrapper would close the cycle
// auth.ts → authedFetch.ts → auth.ts. api/auth.ts re-exports this same object,
// so every existing `vi.spyOn(browserNavigation, "assign")` — auth.test.ts,
// LoginPage.test.tsx — still patches the one instance both callers hold.
export const browserNavigation = {
  assign(url: string): void {
    window.location.assign(url);
  },
};
