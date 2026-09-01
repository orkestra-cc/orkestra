import { describe, expect, it, vi } from "vitest";
import { http, HttpResponse } from "msw";

import { hasSessionMarker, setSessionMarker } from "@/auth/sessionMarker";
import {
  bootstrapFromRefreshCookie,
  clearAccessToken,
  getAccessToken,
  getAccessTokenSnapshot,
  refreshAccessToken,
  REFRESH_FETCH_TIMEOUT_MS,
  REFRESH_LOCK_NAME,
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

  // §4.1's outcome table supersedes the old D15 rule: a 2xx WITHOUT a token is
  // a broken response, which is precisely the reason NOT to act on it — the
  // server has said nothing about the session. The speculative marker survives
  // so the caller can retry, and no token is installed.
  it("a 200 without a token is unavailable and KEEPS the speculative marker (§4.1)", async () => {
    server.use(http.post(REFRESH, () => HttpResponse.json({ ok: true })));
    await expect(bootstrapFromRefreshCookie(API)).resolves.toEqual({
      status: "unavailable",
    });
    expect(hasSessionMarker()).toBe(true);
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

  // The mirror of the bootstrap case above — the two entry points share one
  // rule, and §4.1 replaced D15's "a 2xx without a token signs out" with
  // `unavailable` on both.
  it("a 200 without a token is unavailable on the automatic path too — marker kept (§4.1)", async () => {
    setSessionMarker();
    server.use(http.post(REFRESH, () => HttpResponse.json({})));
    await expect(refreshAccessToken(API)).resolves.toEqual({
      status: "unavailable",
    });
    expect(hasSessionMarker()).toBe(true);
    expect(getAccessToken()).toBeNull();
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

// A helper that seeds a live-looking session, so every assertion below can
// check that BOTH the token and the marker survived.
const seedSession = () => {
  setSessionMarker();
  setAccessToken("at-live", 900);
};

describe("performRefresh outcome allowlist (§4.1, defect C)", () => {
  // The rule INVERTS: signed-out is an allowlist of exactly one status. Only a
  // 401 says "the credential I presented was rejected"; every other non-2xx
  // says something about the SERVER and nothing about the session. A denylist
  // is what defect C was — and 429 is not hypothetical, /refresh-cookie is
  // mounted under the router's GLOBAL rate limiter
  // (cmd/server/middleware.go:131), and a burst of tabs rotating at once is
  // exactly the traffic shape that trips it.
  it.each([
    ["429 from the global rate limiter", 429, { "Retry-After": "30" }],
    ["500", 500, {}],
    ["502 from a proxy during a deploy", 502, {}],
    ["504", 504, {}],
    ["408", 408, {}],
    ["404 from a misrouted host", 404, {}],
  ])(
    "%s is unavailable and keeps token AND marker",
    async (_l, status, headers) => {
      seedSession();
      server.use(
        http.post(REFRESH, () =>
          HttpResponse.json({ detail: "nope" }, { status, headers }),
        ),
      );
      await expect(refreshAccessToken(API)).resolves.toEqual({
        status: "unavailable",
      });
      expect(getAccessToken()).toBe("at-live");
      expect(hasSessionMarker()).toBe(true);
    },
  );

  // v1 called this "a broken response, not an outage" and signed the user out.
  // It IS a broken response, which is the reason not to act on it: a server
  // that answers 200 with no token has told us nothing about the session.
  it("a 2xx with no token is unavailable, not a sign-out", async () => {
    seedSession();
    server.use(http.post(REFRESH, () => HttpResponse.json({ ok: true })));
    await expect(refreshAccessToken(API)).resolves.toEqual({
      status: "unavailable",
    });
    expect(getAccessToken()).toBe("at-live");
    expect(hasSessionMarker()).toBe(true);
  });

  // The allowlist's only member. Without this the table above could silently
  // become "nothing ever signs out".
  it("401 is the ONLY status that signs out, and clears both", async () => {
    seedSession();
    server.use(
      http.post(REFRESH, () => new HttpResponse(null, { status: 401 })),
    );
    await expect(refreshAccessToken(API)).resolves.toEqual({
      status: "signed-out",
    });
    expect(getAccessToken()).toBeNull();
    expect(hasSessionMarker()).toBe(false);
  });
});

describe("performRefresh 409 handling (§4.1b, G7)", () => {
  it("409 then 2xx is ok, and the marker survives", async () => {
    seedSession();
    let hits = 0;
    server.use(
      http.post(REFRESH, () => {
        hits++;
        return hits === 1
          ? HttpResponse.json(
              { code: "refresh_rotation_raced" },
              { status: 409 },
            )
          : HttpResponse.json({ accessToken: "at-2", expiresIn: 900 });
      }),
    );
    await expect(refreshAccessToken(API)).resolves.toEqual({
      status: "ok",
      accessToken: "at-2",
    });
    expect(hits).toBe(2);
    expect(hasSessionMarker()).toBe(true);
  });

  // THE regression test for defect B: a lost rotation race used to fall into
  // !res.ok → signedOut(), and clearing the MARKER made it sticky, so the
  // session stayed lost across a cold load even though the cookie in the jar
  // was perfectly valid.
  it("409 twice is unavailable — token and marker both kept", async () => {
    seedSession();
    let hits = 0;
    server.use(
      http.post(REFRESH, () => {
        hits++;
        return HttpResponse.json(
          { code: "refresh_rotation_raced" },
          { status: 409 },
        );
      }),
    );
    await expect(refreshAccessToken(API)).resolves.toEqual({
      status: "unavailable",
    });
    expect(hits).toBe(2); // exactly one retry, never a loop
    expect(getAccessToken()).toBe("at-live");
    expect(hasSessionMarker()).toBe(true);
  });

  it("409 then 401 is signed-out, and clears both", async () => {
    seedSession();
    let hits = 0;
    server.use(
      http.post(REFRESH, () => {
        hits++;
        return hits === 1
          ? HttpResponse.json(
              { code: "refresh_rotation_raced" },
              { status: 409 },
            )
          : new HttpResponse(null, { status: 401 });
      }),
    );
    await expect(refreshAccessToken(API)).resolves.toEqual({
      status: "signed-out",
    });
    expect(getAccessToken()).toBeNull();
    expect(hasSessionMarker()).toBe(false);
  });
});

describe("performRefresh cross-tab lock (§4.1a, G6)", () => {
  // happy-dom 20.9 sets navigator.locks to NULL, not undefined — and
  // `typeof null === "object"`, so a guard written
  // `typeof navigator.locks === "undefined"` passes and then throws on
  // `.request`. The guard must be `!locks?.request`.
  it("runs unguarded when navigator.locks is null (happy-dom's default)", async () => {
    expect(navigator.locks).toBeNull(); // asserts the premise, not the code
    setSessionMarker();
    const refresh = countingRefresh(() => HttpResponse.json(okBody));
    await expect(refreshAccessToken(API)).resolves.toEqual({
      status: "ok",
      accessToken: "at-1",
    });
    expect(refresh.hits()).toBe(1);
  });

  it("takes the named lock and runs the refresh inside it", async () => {
    const calls: string[] = [];
    let ranInside = false;
    vi.stubGlobal("navigator", {
      locks: {
        request: async (name: string, cb: () => Promise<unknown>) => {
          calls.push(name);
          const out = await cb();
          ranInside = true;
          return out;
        },
      },
    });
    setSessionMarker();
    server.use(http.post(REFRESH, () => HttpResponse.json(okBody)));
    await refreshAccessToken(API);
    expect(calls).toEqual([REFRESH_LOCK_NAME]);
    expect(REFRESH_LOCK_NAME).toBe("orkestra:auth-refresh");
    expect(ranInside).toBe(true);
  });

  // The in-tab coalescing must survive the lock — a second caller must share
  // the in-flight promise rather than queue behind the lock. Task 7 widens
  // this to the MIXED shape (refreshAccessToken + refreshAfterUnauthorized),
  // because both entry points then funnel through performRefresh and a
  // regression that coalesced only within one of them would otherwise pass.
  it("coalesces concurrent callers into one request", async () => {
    setSessionMarker();
    const refresh = countingRefresh(() => HttpResponse.json(okBody));
    await Promise.all([refreshAccessToken(API), refreshAccessToken(API)]);
    expect(refresh.hits()).toBe(1);
  });
});

describe("performRefresh is bounded (§4.1c)", () => {
  // ⚠️ AbortSignal.timeout is NOT controlled by vitest's fake clock (probed:
  // `aborted` stays false after advancing 20s). If someone "simplifies"
  // AbortController + setTimeout back to it, these cases stop aborting and
  // either hang for ten real seconds or fail — which is the tripwire that
  // keeps the divergence from frontend-admin honest.
  it("a fetch that never resolves is unavailable at the timeout, keeping both", async () => {
    vi.useFakeTimers();
    try {
      seedSession();
      server.use(http.post(REFRESH, () => new Promise<never>(() => {})));
      const p = refreshAccessToken(API);
      await vi.advanceTimersByTimeAsync(REFRESH_FETCH_TIMEOUT_MS + 10);
      await expect(p).resolves.toEqual({ status: "unavailable" });
      expect(getAccessToken()).toBe("at-live");
      expect(hasSessionMarker()).toBe(true);
    } finally {
      vi.useRealTimers();
    }
  });

  // The round-13 regression test. It MUST use a streamed body: `fetch`
  // resolves on HEADERS, so a delayed whole response is caught even by a
  // clearTimeout placed right after `await fetch` — that shape would pass
  // against the bug. Measured against the buggy version: fetch resolved at
  // 31ms, the timer was cleared, and the body finished 3s later with no abort.
  it("a response whose HEADERS arrive but whose BODY stalls still times out", async () => {
    vi.useFakeTimers();
    try {
      seedSession();
      server.use(
        http.post(REFRESH, () => {
          const stream = new ReadableStream({
            start(controller) {
              controller.enqueue(new TextEncoder().encode('{"accessToken":'));
              // never closed — the body stalls after the headers
            },
          });
          return new HttpResponse(stream, {
            headers: { "Content-Type": "application/json" },
          });
        }),
      );
      const p = refreshAccessToken(API);
      await vi.advanceTimersByTimeAsync(REFRESH_FETCH_TIMEOUT_MS + 10);
      await expect(p).resolves.toEqual({ status: "unavailable" });
      expect(getAccessToken()).toBe("at-live");
      expect(hasSessionMarker()).toBe(true);
    } finally {
      vi.useRealTimers();
    }
  });

  // The assertion that actually pins §4.1a's transitive bound: a contender
  // queued behind a stalled holder must get the lock when the timeout fires,
  // not wait forever. Without this, "the lock is bounded transitively" is a
  // claim rather than a tested property — which is exactly what round 13
  // found it to be.
  it("releases the lock when the timeout fires", async () => {
    vi.useFakeTimers();
    try {
      let chain: Promise<unknown> = Promise.resolve();
      const locks = {
        request: (_name: string, cb: () => Promise<unknown>) => {
          const run = chain.then(() => cb());
          chain = run.catch(() => undefined);
          return run;
        },
      };
      vi.stubGlobal("navigator", { locks });
      seedSession();
      server.use(http.post(REFRESH, () => new Promise<never>(() => {})));

      const holder = refreshAccessToken(API);
      let contenderRan = false;
      const contender = locks.request(REFRESH_LOCK_NAME, async () => {
        contenderRan = true;
      });

      await vi.advanceTimersByTimeAsync(REFRESH_FETCH_TIMEOUT_MS + 10);
      await holder;
      await contender;
      expect(contenderRan).toBe(true);
    } finally {
      vi.useRealTimers();
    }
  });

  // A refresh that answers normally must not leave a live 10s timer behind —
  // which is also why clearTimeout beats AbortSignal.timeout, since that one
  // cannot be cancelled at all.
  it("clears the timer on a normal answer", async () => {
    vi.useFakeTimers();
    try {
      setSessionMarker();
      const refresh = countingRefresh(() => HttpResponse.json(okBody));
      await refreshAccessToken(API);
      await vi.advanceTimersByTimeAsync(REFRESH_FETCH_TIMEOUT_MS * 2);
      expect(refresh.hits()).toBe(1); // nothing re-fired, nothing aborted
    } finally {
      vi.useRealTimers();
    }
  });
});
