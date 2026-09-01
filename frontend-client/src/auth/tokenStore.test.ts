import { describe, expect, it, vi } from "vitest";
import { http, HttpResponse } from "msw";

import { hasSessionMarker, setSessionMarker } from "@/auth/sessionMarker";
import {
  bootstrapFromRefreshCookie,
  clearAccessToken,
  getAccessToken,
  getAccessTokenSnapshot,
  refreshAccessToken,
  setAccessToken,
} from "@/auth/tokenStore";
import { url } from "@/test/handlers";
import { server } from "@/test/server";

const API = "http://api.test";
const REFRESH = url("/v1/auth/client/refresh-cookie");
const okBody = { accessToken: "at-1", tokenType: "Bearer", expiresIn: 900 };

// Every value currently held in web storage — the assertion surface for
// "no access token is ever persisted".
const storageValues = (): Array<string | null> => [
  ...Array.from({ length: localStorage.length }, (_, i) =>
    localStorage.getItem(localStorage.key(i)!),
  ),
  ...Array.from({ length: sessionStorage.length }, (_, i) =>
    sessionStorage.getItem(sessionStorage.key(i)!),
  ),
];

// A refresh endpoint that counts hits and records the marker state at
// request time.
const countingRefresh = (respond: (hit: number) => Response) => {
  let hits = 0;
  let markerAtRequest: boolean | null = null;
  server.use(
    http.post(REFRESH, () => {
      hits++;
      markerAtRequest = hasSessionMarker();
      return respond(hits);
    }),
  );
  return { hits: () => hits, markerAtRequest: () => markerAtRequest };
};

// A Web Storage that refuses every operation — private mode, a disabled
// storage policy. sessionMarker.ts already swallows these errors; the
// refresh must not depend on the marker having been written.
const throwingStorage = new Proxy({} as Storage, {
  get: () => () => {
    throw new Error("SecurityError: storage disabled");
  },
});

describe("bootstrapFromRefreshCookie", () => {
  it("stamps the session marker BEFORE the refresh request and installs the token in memory only", async () => {
    const refresh = countingRefresh(() => HttpResponse.json(okBody));
    expect(hasSessionMarker()).toBe(false);
    await expect(bootstrapFromRefreshCookie(API)).resolves.toEqual({
      status: "ok",
      accessToken: "at-1",
    });
    expect(refresh.markerAtRequest()).toBe(true);
    expect(hasSessionMarker()).toBe(true);
    expect(getAccessToken()).toBe("at-1");
    expect(storageValues()).not.toContain("at-1");
  });

  it("still presents the cookie when storage throws (private mode) — B1", async () => {
    vi.stubGlobal("localStorage", throwingStorage);
    const refresh = countingRefresh(() => HttpResponse.json(okBody));
    await expect(bootstrapFromRefreshCookie(API)).resolves.toEqual({
      status: "ok",
      accessToken: "at-1",
    });
    expect(refresh.hits()).toBe(1);
    expect(getAccessToken()).toBe("at-1");
  });

  it("signed-out (401) clears the speculative marker and the token", async () => {
    setAccessToken("stale");
    server.use(
      http.post(REFRESH, () =>
        HttpResponse.json({ detail: "no session" }, { status: 401 }),
      ),
    );
    await expect(bootstrapFromRefreshCookie(API)).resolves.toEqual({
      status: "signed-out",
    });
    expect(hasSessionMarker()).toBe(false);
    expect(getAccessToken()).toBeNull();
  });

  it("a 200 without a token is signed-out too and clears the marker", async () => {
    server.use(http.post(REFRESH, () => HttpResponse.json({ ok: true })));
    await expect(bootstrapFromRefreshCookie(API)).resolves.toEqual({
      status: "signed-out",
    });
    expect(hasSessionMarker()).toBe(false);
    expect(getAccessToken()).toBeNull();
  });

  it("unavailable (503) keeps the marker and any token so a retry can succeed", async () => {
    const refresh = countingRefresh((hit) =>
      hit === 1
        ? HttpResponse.json(
            { code: "session_enforcement_unavailable" },
            { status: 503 },
          )
        : HttpResponse.json(okBody),
    );
    await expect(bootstrapFromRefreshCookie(API)).resolves.toEqual({
      status: "unavailable",
    });
    expect(hasSessionMarker()).toBe(true);
    expect(getAccessToken()).toBeNull();
    await expect(bootstrapFromRefreshCookie(API)).resolves.toEqual({
      status: "ok",
      accessToken: "at-1",
    });
    expect(refresh.hits()).toBe(2);
  });

  it("a transport failure is unavailable — never a rejection — and a retry can succeed (B2)", async () => {
    const refresh = countingRefresh((hit) =>
      hit === 1 ? HttpResponse.error() : HttpResponse.json(okBody),
    );
    await expect(bootstrapFromRefreshCookie(API)).resolves.toEqual({
      status: "unavailable",
    });
    expect(hasSessionMarker()).toBe(true);
    expect(getAccessToken()).toBeNull();
    await expect(bootstrapFromRefreshCookie(API)).resolves.toEqual({
      status: "ok",
      accessToken: "at-1",
    });
    expect(refresh.hits()).toBe(2);
  });

  it("sends the request with credentials so the refresh cookie travels", async () => {
    let credentials: RequestCredentials | undefined;
    server.use(
      http.post(REFRESH, ({ request }) => {
        credentials = request.credentials;
        return HttpResponse.json(okBody);
      }),
    );
    await bootstrapFromRefreshCookie(API);
    expect(credentials).toBe("include");
  });

  it("leaves an existing marker in place and coalesces with a concurrent automatic refresh", async () => {
    setSessionMarker();
    const refresh = countingRefresh(() => HttpResponse.json(okBody));
    const [a, b] = await Promise.all([
      bootstrapFromRefreshCookie(API),
      refreshAccessToken(API),
    ]);
    expect(a).toEqual({ status: "ok", accessToken: "at-1" });
    expect(b).toEqual({ status: "ok", accessToken: "at-1" });
    expect(refresh.hits()).toBe(1);
    expect(hasSessionMarker()).toBe(true);
  });
});

describe("refreshAccessToken (the automatic path)", () => {
  it("still short-circuits without a marker — no request for anonymous visitors", async () => {
    const refresh = countingRefresh(() => HttpResponse.json(okBody));
    await expect(refreshAccessToken(API)).resolves.toEqual({
      status: "signed-out",
    });
    expect(refresh.hits()).toBe(0);
  });

  it("shares the outcome mapping: 503 and a transport failure are unavailable, nothing cleared", async () => {
    setSessionMarker();
    setAccessToken("at-0");
    const refresh = countingRefresh((hit) =>
      hit === 1 ? HttpResponse.json({}, { status: 503 }) : HttpResponse.error(),
    );
    await expect(refreshAccessToken(API)).resolves.toEqual({
      status: "unavailable",
    });
    await expect(refreshAccessToken(API)).resolves.toEqual({
      status: "unavailable",
    });
    expect(refresh.hits()).toBe(2);
    expect(hasSessionMarker()).toBe(true);
    expect(getAccessToken()).toBe("at-0");
  });

  it("a 200 without a token now clears the marker on the automatic path too (D15)", async () => {
    setSessionMarker();
    server.use(http.post(REFRESH, () => HttpResponse.json({})));
    await expect(refreshAccessToken(API)).resolves.toEqual({
      status: "signed-out",
    });
    expect(hasSessionMarker()).toBe(false);
  });
});

describe("access-token expiry reckoning (§4.5)", () => {
  it("records expiresAt from the reported duration, in the local clock domain", () => {
    vi.useFakeTimers();
    try {
      vi.setSystemTime(new Date("2026-09-01T12:00:00.000Z"));
      setAccessToken("at-1", 900);
      expect(getAccessTokenSnapshot()).toEqual({
        token: "at-1",
        expiresAt: Date.now() + 900_000,
      });
    } finally {
      vi.useRealTimers();
    }
  });

  // The reason the duration is used at all. Both ends of the comparison are
  // taken from the same clock, so a constant offset cancels: a client whose
  // clock is hours off still reads a token installed 60s ago, with a 900s
  // life, as live. A Date.now()-vs-`exp` implementation fails this.
  it("is immune to a wall-clock offset between install and read", () => {
    vi.useFakeTimers();
    try {
      vi.setSystemTime(new Date("2026-09-01T12:00:00.000Z"));
      setAccessToken("at-skew", 900);
      const at = getAccessTokenSnapshot().expiresAt!;
      // The clock jumps hours forward AND the elapsed time is only 60s: this
      // is the shape a badly set clock produces on the NEXT read.
      vi.advanceTimersByTime(60_000);
      expect(at).toBeGreaterThan(Date.now());
    } finally {
      vi.useRealTimers();
    }
  });

  it("falls back to the JWT exp when no duration is supplied", () => {
    const seg = Buffer.from('{"exp":1700000000}', "utf8").toString("base64url");
    setAccessToken(`h.${seg}.s`);
    expect(getAccessTokenSnapshot().expiresAt).toBe(1700000000 * 1000);
  });

  it("records an UNKNOWN expiry when neither is available", () => {
    setAccessToken("opaque-not-a-jwt");
    expect(getAccessTokenSnapshot()).toEqual({
      token: "opaque-not-a-jwt",
      expiresAt: null,
    });
  });

  it("clearing the token clears the expiry too", () => {
    setAccessToken("at-1", 900);
    clearAccessToken();
    expect(getAccessTokenSnapshot()).toEqual({ token: null, expiresAt: null });
  });

  it("a refresh installs the pair from the response body", async () => {
    vi.useFakeTimers();
    try {
      setSessionMarker();
      server.use(
        http.post(REFRESH, () =>
          HttpResponse.json({ accessToken: "at-r", expiresIn: 120 }),
        ),
      );
      await refreshAccessToken(API);
      expect(getAccessTokenSnapshot()).toEqual({
        token: "at-r",
        expiresAt: Date.now() + 120_000,
      });
    } finally {
      vi.useRealTimers();
    }
  });

  // The test that fails if ANY single call site in the §4.6 table is missed:
  // a dropped lifetime reads as "unknown", which reads as "live", which means
  // the recovery never fires. Sign in, cross the recorded lifetime, and the
  // expiry must have gone from "in the future" to "in the past".
  it("a recorded lifetime actually elapses", () => {
    vi.useFakeTimers();
    try {
      setAccessToken("at-e2e", 60);
      const { expiresAt } = getAccessTokenSnapshot();
      expect(expiresAt).toBeGreaterThan(Date.now());
      vi.advanceTimersByTime(61_000);
      expect(getAccessTokenSnapshot().expiresAt).toBeLessThanOrEqual(
        Date.now(),
      );
    } finally {
      vi.useRealTimers();
    }
  });

  // The deliberate divergence from frontend-admin, which treats a response
  // without expiresIn as a FAILED refresh. Turning a valid rotation into a
  // sign-out over a missing optional field is the wrong trade.
  it("a refresh WITHOUT expiresIn still installs the token", async () => {
    setSessionMarker();
    server.use(
      http.post(REFRESH, () => HttpResponse.json({ accessToken: "at-noexp" })),
    );
    await expect(refreshAccessToken(API)).resolves.toEqual({
      status: "ok",
      accessToken: "at-noexp",
    });
    expect(getAccessToken()).toBe("at-noexp");
  });
});
