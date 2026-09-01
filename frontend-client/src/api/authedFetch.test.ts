import { describe, expect, it, vi } from "vitest";
import { http, HttpResponse } from "msw";

import { authedFetch } from "@/api/authedFetch";
import {
  clearSessionLocally,
  getAccessToken,
  getAccessTokenSnapshot,
  setAccessToken,
} from "@/auth/tokenStore";
import { hasSessionMarker, setSessionMarker } from "@/auth/sessionMarker";
import { url } from "@/test/handlers";
import { server } from "@/test/server";

const REFRESH = url("/v1/auth/client/refresh-cookie");
const THING = url("/v1/me/thing");
const CHANGE_PASSWORD = url("/v1/auth/client/change-password");

// Records what actually reached the wire for /v1/me/thing.
const recordRequests = (respond: (hit: number, req: Request) => Response) => {
  const seen: Request[] = [];
  server.use(
    http.all(THING, ({ request }) => {
      seen.push(request.clone());
      return respond(seen.length, request);
    }),
  );
  return {
    seen,
    header: (i: number, name: string) => seen[i]?.headers.get(name) ?? null,
  };
};

const countRefresh = (respond: (hit: number) => Response) => {
  let hits = 0;
  server.use(
    http.post(REFRESH, () => {
      hits++;
      return respond(hits);
    }),
  );
  return { hits: () => hits };
};

// A token whose life is under our control. The lifetime is what the tests
// actually manipulate; the string itself never has to be a real JWT because
// setAccessToken's duration argument short-circuits the jwtExp fallback.
const seedToken = (token: string, expiresInSeconds: number) => {
  setSessionMarker();
  setAccessToken(token, expiresInSeconds);
};
const seedExpiredToken = (token = "at-old") => {
  setSessionMarker();
  setAccessToken(token, -1); // expiresAt is already in the past
};

describe("authedFetch header merging (§4.2)", () => {
  it("sends headers given as a Headers instance", async () => {
    const rec = recordRequests(() => HttpResponse.json({ ok: true }));
    seedToken("at-1", 900);
    await authedFetch("/v1/me/thing", {
      headers: new Headers({ "X-Custom": "kept" }),
    });
    // Fails against an object-spread implementation: spreading a Headers
    // instance yields {} — it has no own enumerable properties — so every
    // header the caller set is dropped SILENTLY.
    expect(rec.header(0, "X-Custom")).toBe("kept");
  });

  it("sends headers given as an array of tuples", async () => {
    const rec = recordRequests(() => HttpResponse.json({ ok: true }));
    seedToken("at-1", 900);
    await authedFetch("/v1/me/thing", { headers: [["X-Custom", "kept"]] });
    expect(rec.header(0, "X-Custom")).toBe("kept");
  });

  it("sends headers given as a plain object (the migration's regression guard)", async () => {
    const rec = recordRequests(() => HttpResponse.json({ ok: true }));
    seedToken("at-1", 900);
    await authedFetch("/v1/me/thing", { headers: { "X-Custom": "kept" } });
    expect(rec.header(0, "X-Custom")).toBe("kept");
  });

  it("defaults Accept and Content-Type only when absent", async () => {
    const rec = recordRequests(() => HttpResponse.json({ ok: true }));
    seedToken("at-1", 900);
    await authedFetch("/v1/me/thing", {
      method: "POST",
      body: JSON.stringify({ a: 1 }),
    });
    expect(rec.header(0, "Accept")).toBe("application/json");
    expect(rec.header(0, "Content-Type")).toBe("application/json");

    await authedFetch("/v1/me/thing", {
      method: "POST",
      body: JSON.stringify({ a: 1 }),
      headers: {
        Accept: "text/csv",
        "Content-Type": "application/merge-patch+json",
      },
    });
    expect(rec.header(1, "Accept")).toBe("text/csv");
    expect(rec.header(1, "Content-Type")).toBe("application/merge-patch+json");
  });

  it("does NOT set Content-Type for a FormData body", async () => {
    const rec = recordRequests(() => HttpResponse.json({ ok: true }));
    seedToken("at-1", 900);
    const fd = new FormData();
    fd.append("f", "v");
    await authedFetch("/v1/me/thing", { method: "POST", body: fd });
    // Forcing application/json on FormData destroys the multipart boundary.
    // fetch sets its own with the boundary; assert it is NOT ours.
    expect(rec.header(0, "Content-Type")).not.toBe("application/json");
  });

  // The other half of the §4.2 rule: Content-Type is set for a body we KNOW is
  // JSON, and `typeof body === "string"` is the whole narrowing. A bodiless
  // POST is not a JSON body, so the helper must add nothing — a Content-Type on
  // an empty body is a claim about a payload that does not exist.
  it("does NOT set Content-Type for a POST with no body", async () => {
    const rec = recordRequests(() => HttpResponse.json({ ok: true }));
    seedToken("at-1", 900);
    await authedFetch("/v1/me/thing", { method: "POST" });
    expect(rec.header(0, "Content-Type")).toBeNull();
    // …and the Accept default is unaffected: the two rules are independent.
    expect(rec.header(0, "Accept")).toBe("application/json");
  });

  const authorizationShapes: [string, HeadersInit][] = [
    ["a plain object", { Authorization: "Bearer caller" }],
    ["a Headers instance", new Headers({ Authorization: "Bearer caller" })],
    ["an array of tuples", [["Authorization", "Bearer caller"]]],
  ];
  it.each(authorizationShapes)(
    "overrides a caller-supplied Authorization given as %s",
    async (_l, headers) => {
      const rec = recordRequests(() => HttpResponse.json({ ok: true }));
      seedToken("at-1", 900);
      await authedFetch("/v1/me/thing", { headers });
      // The precedence decision, tested where it can actually break. avatar.ts
      // let the bearer win, billingProfile.ts let caller headers win — a
      // divergence that existed only because each wrapper chose its own spread
      // order. `set` last, always.
      expect(rec.header(0, "Authorization")).toBe("Bearer at-1");
    },
  );

  it("sends no Authorization at all when the store is empty", async () => {
    const rec = recordRequests(() => HttpResponse.json({ ok: true }));
    // no token, no marker
    await authedFetch("/v1/me/thing");
    expect(rec.header(0, "Authorization")).toBeNull();
  });
});

describe("authedFetch 401 recovery (§4.3)", () => {
  it("expired token → 401 → refresh → retry carries the NEW bearer", async () => {
    const refresh = countRefresh(() =>
      HttpResponse.json({ accessToken: "at-new", expiresIn: 900 }),
    );
    const rec = recordRequests((hit) =>
      hit === 1
        ? new HttpResponse(null, { status: 401 })
        : HttpResponse.json({ ok: true }),
    );
    seedExpiredToken("at-old");

    const res = await authedFetch("/v1/me/thing");
    expect(res.status).toBe(200);
    expect(refresh.hits()).toBe(1);
    expect(rec.seen.length).toBe(2);
    expect(rec.header(0, "Authorization")).toBe("Bearer at-old");
    expect(rec.header(1, "Authorization")).toBe("Bearer at-new");
  });

  // THE lockout-hazard regression test (§4.4). change-password is an
  // AUTHENTICATED endpoint that answers 401 when the CURRENT PASSWORD IN THE
  // BODY is wrong; a blanket "401 → refresh → retry" re-sends the failed
  // attempt and the backend counts it again, so a user who mistypes twice is
  // locked out as though they had tried four times.
  //
  // The fixture's remaining life IS the test: a token with 20 minutes left
  // passes against the broken implementation too. 20s is inside any plausible
  // margin — it is precisely the value the removed 30s SKEW would have
  // mis-classified as "not live".
  it("a live token's 401 is passed through — no refresh, no replay", async () => {
    const refresh = countRefresh(() => HttpResponse.json({ accessToken: "x" }));
    const rec = recordRequests(() => HttpResponse.json({ ok: true }));
    let attempts = 0;
    server.use(
      http.post(CHANGE_PASSWORD, () => {
        attempts++;
        return HttpResponse.json(
          { detail: "Invalid email or password" },
          { status: 401 },
        );
      }),
    );
    seedToken("at-live", 20);
    // The fixture asserts its own premise: 20s of life, which the server still
    // accepts, so the handler DID run and counted the failed attempt.
    const { expiresAt } = getAccessTokenSnapshot();
    expect(expiresAt! - Date.now()).toBeGreaterThan(15_000);
    expect(expiresAt! - Date.now()).toBeLessThan(25_000);

    const res = await authedFetch("/v1/auth/client/change-password", {
      method: "POST",
      body: JSON.stringify({ currentPassword: "wrong", newPassword: "x" }),
    });
    expect(res.status).toBe(401);
    expect(refresh.hits()).toBe(0);
    expect(attempts).toBe(1); // the failed attempt was NOT replayed
    expect(rec.seen.length).toBe(0); // it went to change-password, not /thing
  });

  // Pins that the comparison is `<=` at the exact instant and carries no
  // hidden margin. A margin here IS the round-11 replay hole.
  it("expiresAt === sentAt counts as expired; sentAt + 1 counts as live", async () => {
    vi.useFakeTimers();
    try {
      const refresh = countRefresh(() =>
        HttpResponse.json({ accessToken: "at-new", expiresIn: 900 }),
      );
      recordRequests((hit) =>
        hit % 2 === 1
          ? new HttpResponse(null, { status: 401 })
          : HttpResponse.json({ ok: true }),
      );
      setSessionMarker();

      setAccessToken("at-boundary", 0); // expiresAt === Date.now()
      await authedFetch("/v1/me/thing");
      expect(refresh.hits()).toBe(1);

      setAccessToken("at-live", 0.001); // expiresAt === Date.now() + 1ms
      await authedFetch("/v1/me/thing");
      expect(refresh.hits()).toBe(1); // unchanged — passed through
    } finally {
      vi.useRealTimers();
    }
  });

  // The direction that FLIPPED in round 11. An unknown expiry cannot prove the
  // handler never ran, and under a rule whose failure mode is a REPLAY rather
  // than a wasted refresh, "don't know" falls on the safe side.
  it("an UNKNOWN expiry is treated as live — passed through, no refresh", async () => {
    const refresh = countRefresh(() => HttpResponse.json({ accessToken: "x" }));
    recordRequests(() => new HttpResponse(null, { status: 401 }));
    setSessionMarker();
    setAccessToken("opaque-not-a-jwt"); // no duration, unreadable exp
    expect(getAccessTokenSnapshot().expiresAt).toBeNull();

    const res = await authedFetch("/v1/me/thing", {
      method: "POST",
      body: "{}",
    });
    expect(res.status).toBe(401);
    expect(refresh.hits()).toBe(0);
  });

  // R1 / §3.D. The server states it rejected the bearer BEFORE dispatch, so
  // the request provably never reached its handler — proof enough on its own,
  // and it recovers the token that expired IN FLIGHT, which the client-side
  // reckoning has to give up. Fails against a reckoning-only implementation.
  it("recovers a LIVE token's 401 when the server says access_token_expired", async () => {
    const refresh = countRefresh(() =>
      HttpResponse.json({ accessToken: "at-new", expiresIn: 900 }),
    );
    const rec = recordRequests((hit) =>
      hit === 1
        ? HttpResponse.json(
            {
              status: 401,
              title: "access token expired",
              code: "access_token_expired",
            },
            { status: 401 },
          )
        : HttpResponse.json({ ok: true }),
    );
    seedToken("at-inflight", 900); // still live by our own reckoning

    const res = await authedFetch("/v1/me/thing");
    expect(res.status).toBe(200);
    expect(refresh.hits()).toBe(1);
    expect(rec.header(1, "Authorization")).toBe("Bearer at-new");
    // access_token_expired is the OPPOSITE of terminal: it must refresh and
    // clear NOTHING. The two membership tests read the same field, so an
    // if/else chain written in the wrong order would collapse them (§6).
    expect(getAccessToken()).toBe("at-new");
    expect(hasSessionMarker()).toBe(true);
  });

  // Where the two proofs visibly COMPOSE rather than merely coexist: proof (1)
  // does not consult the expiry at all, so an unknown expiry — which on its own
  // reads as live and is passed through (the case above) — still recovers.
  it("an UNKNOWN expiry plus access_token_expired is recovered", async () => {
    const refresh = countRefresh(() =>
      HttpResponse.json({ accessToken: "at-new", expiresIn: 900 }),
    );
    const rec = recordRequests((hit) =>
      hit === 1
        ? HttpResponse.json({ code: "access_token_expired" }, { status: 401 })
        : HttpResponse.json({ ok: true }),
    );
    setSessionMarker();
    setAccessToken("opaque-not-a-jwt"); // no duration, unreadable exp
    expect(getAccessTokenSnapshot().expiresAt).toBeNull();

    const res = await authedFetch("/v1/me/thing");
    expect(res.status).toBe(200);
    expect(refresh.hits()).toBe(1);
    expect(rec.header(1, "Authorization")).toBe("Bearer at-new");
  });

  const refreshOutages: [string, number][] = [
    ["503", 503],
    ["429", 429],
  ];
  it.each(refreshOutages)(
    "refresh %s → the caller sees the original 401 and the token survives (G2)",
    async (_l, status) => {
      countRefresh(() => new HttpResponse(null, { status }));
      recordRequests(() => new HttpResponse(null, { status: 401 }));
      seedExpiredToken("at-old");

      const res = await authedFetch("/v1/me/thing");
      expect(res.status).toBe(401);
      expect(getAccessToken()).toBe("at-old");
      expect(hasSessionMarker()).toBe(true);
    },
  );

  it("refresh 409 twice → original 401, marker and token KEPT (G7)", async () => {
    const refresh = countRefresh(() =>
      HttpResponse.json({ code: "refresh_rotation_raced" }, { status: 409 }),
    );
    recordRequests(() => new HttpResponse(null, { status: 401 }));
    seedExpiredToken("at-old");

    const res = await authedFetch("/v1/me/thing");
    expect(res.status).toBe(401);
    expect(refresh.hits()).toBe(2);
    expect(getAccessToken()).toBe("at-old");
    expect(hasSessionMarker()).toBe(true);
  });

  it("refresh 401 → token and marker cleared so AuthProvider can re-render (G3)", async () => {
    countRefresh(() => new HttpResponse(null, { status: 401 }));
    recordRequests(() => new HttpResponse(null, { status: 401 }));
    seedExpiredToken("at-old");

    const res = await authedFetch("/v1/me/thing");
    expect(res.status).toBe(401);
    expect(getAccessToken()).toBeNull();
    expect(hasSessionMarker()).toBe(false);
  });

  // Fails against a v3-shaped implementation, which routed everything through
  // the marker-gated refreshAccessToken, returned the raw 401 and left the
  // user signed-in-but-broken.
  it("expired token with NO marker still attempts the refresh (branch 4a)", async () => {
    const refresh = countRefresh(() =>
      HttpResponse.json({ accessToken: "at-new", expiresIn: 900 }),
    );
    const rec = recordRequests((hit) =>
      hit === 1
        ? new HttpResponse(null, { status: 401 })
        : HttpResponse.json({ ok: true }),
    );
    setAccessToken("at-old", -1); // NO setSessionMarker
    expect(hasSessionMarker()).toBe(false);

    const res = await authedFetch("/v1/me/thing");
    expect(res.status).toBe(200);
    expect(refresh.hits()).toBe(1);
    expect(rec.header(1, "Authorization")).toBe("Bearer at-new");
  });

  it("no bearer and no marker (anonymous) → ZERO refresh requests (branch 4b)", async () => {
    const refresh = countRefresh(() => HttpResponse.json({ accessToken: "x" }));
    recordRequests(() => new HttpResponse(null, { status: 401 }));
    // no token, no marker

    const res = await authedFetch("/v1/me/thing");
    expect(res.status).toBe(401);
    expect(refresh.hits()).toBe(0);
  });

  it("a burst of three 401s produces exactly one /refresh-cookie", async () => {
    const refresh = countRefresh(() =>
      HttpResponse.json({ accessToken: "at-new", expiresIn: 900 }),
    );
    server.use(
      http.get(THING, ({ request }) =>
        request.headers.get("Authorization") === "Bearer at-new"
          ? HttpResponse.json({ ok: true })
          : new HttpResponse(null, { status: 401 }),
      ),
    );
    seedExpiredToken("at-old");

    const all = await Promise.all([
      authedFetch("/v1/me/thing"),
      authedFetch("/v1/me/thing"),
      authedFetch("/v1/me/thing"),
    ]);
    expect(all.map((r) => r.status)).toEqual([200, 200, 200]);
    expect(refresh.hits()).toBe(1);
  });
});

describe("authedFetch terminal codes (§4.3 branch 1, §3.C)", () => {
  it.each([["session_revoked"], ["session_max_age_reached"]])(
    "%s on a LIVE token clears everything with no refresh",
    async (code) => {
      const refresh = countRefresh(() =>
        HttpResponse.json({ accessToken: "x" }),
      );
      const rec = recordRequests(() =>
        HttpResponse.json({ code }, { status: 401 }),
      );
      seedToken("at-live", 900);

      const res = await authedFetch("/v1/me/thing");
      expect(res.status).toBe(401);
      // A token minted from the same cookie carries the same dead sid, so a
      // refresh is pointless — not merely wasteful.
      expect(refresh.hits()).toBe(0);
      expect(rec.seen.length).toBe(1); // no retry either
      expect(getAccessToken()).toBeNull();
      expect(hasSessionMarker()).toBe(false);
    },
  );

  // THE test that fails against `if (body.code) → clear`, the simplification
  // §3.C exists to forbid. The middleware emits at least seven top-level
  // codes and four of them ride on 401s that are emphatically not a dead
  // session — turning four recoverable prompts into a logout.
  it.each([
    ["step_up_required"],
    ["mfa_enrollment_required"],
    ["password_confirm_required"],
  ])(
    "a non-terminal code (%s) on a live token is passed through untouched",
    async (code) => {
      const refresh = countRefresh(() =>
        HttpResponse.json({ accessToken: "x" }),
      );
      recordRequests(() => HttpResponse.json({ code }, { status: 401 }));
      seedToken("at-live", 900);

      const res = await authedFetch("/v1/me/thing");
      expect(res.status).toBe(401);
      expect(refresh.hits()).toBe(0);
      expect(getAccessToken()).toBe("at-live");
      expect(hasSessionMarker()).toBe(true);
    },
  );

  // Pins that the implementation reads the TOP LEVEL and not errors[0].value.
  // The generic sendErrorResponse shape puts appErr.Code there, and for an
  // AuthenticationError that value is CodeInvalidCredentials — the same value
  // a wrong password produces, so it discriminates nothing.
  it("a code that lives only at errors[0].value is NOT terminal", async () => {
    recordRequests(() =>
      HttpResponse.json(
        {
          status: 401,
          title: "Unauthorized",
          detail: "authentication required",
          errors: [
            {
              message: "x",
              location: "require_auth",
              value: "INVALID_CREDENTIALS",
            },
          ],
        },
        { status: 401 },
      ),
    );
    seedToken("at-live", 900);
    const res = await authedFetch("/v1/me/thing");
    expect(res.status).toBe(401);
    expect(getAccessToken()).toBe("at-live");
  });

  it("a retry that 401s with a terminal code clears, with exactly one refresh and no second retry", async () => {
    const refresh = countRefresh(() =>
      HttpResponse.json({ accessToken: "at-new", expiresIn: 900 }),
    );
    // The FIRST 401 must be codeless, or branch 1 ends the call before a
    // refresh ever happens and the case tests nothing about the retry.
    const rec = recordRequests((hit) =>
      hit === 1
        ? new HttpResponse(null, { status: 401 })
        : HttpResponse.json({ code: "session_revoked" }, { status: 401 }),
    );
    seedExpiredToken("at-old");

    const res = await authedFetch("/v1/me/thing");
    expect(res.status).toBe(401);
    expect(refresh.hits()).toBe(1);
    expect(rec.seen.length).toBe(2); // original + ONE retry
    expect(getAccessToken()).toBeNull();
    expect(hasSessionMarker()).toBe(false);
  });

  // The retry cap, pinned where it can actually break. The refreshed token is
  // itself ALREADY EXPIRED, so an implementation that "retries" by recursing
  // into authedFetch would find proof (b) true a second time and rotate again —
  // and again. retryOnce is a single fetch: one rotation, one retry, then the
  // caller gets the 401 whatever it says.
  it("never fires a second refresh, even when the refreshed token is itself expired", async () => {
    const refresh = countRefresh(() =>
      HttpResponse.json({ accessToken: "at-new", expiresIn: -1 }),
    );
    const rec = recordRequests(() => new HttpResponse(null, { status: 401 }));
    seedExpiredToken("at-old");

    const res = await authedFetch("/v1/me/thing");
    expect(res.status).toBe(401);
    expect(refresh.hits()).toBe(1); // ONE rotation for the whole call
    expect(rec.seen.length).toBe(2); // original + exactly ONE retry
    expect(rec.header(1, "Authorization")).toBe("Bearer at-new");
  });

  // Guards against "any retry 401 means signed out", which would sign out the
  // §4.4 mirror case for mistyping a password.
  it("a retry that 401s with NO code changes nothing", async () => {
    const refresh = countRefresh(() =>
      HttpResponse.json({ accessToken: "at-new", expiresIn: 900 }),
    );
    const rec = recordRequests(() => new HttpResponse(null, { status: 401 }));
    seedExpiredToken("at-old");

    const res = await authedFetch("/v1/me/thing");
    expect(res.status).toBe(401);
    expect(rec.seen.length).toBe(2);
    expect(refresh.hits()).toBe(1); // never a SECOND refresh
    expect(getAccessToken()).toBe("at-new"); // the refresh's token survives
    expect(hasSessionMarker()).toBe(true);
  });
});

describe("authedFetch body integrity (§5.13)", () => {
  // The assertion is on the CALLER'S view, not on bodyUsed. readError does
  // `await res.json()` wrapped in `.catch(() => ({}))`, so on an already-read
  // body the TypeError is SWALLOWED and the caller silently gets the fallback
  // message with no code at all — error branches quietly stop matching rather
  // than crashing.
  const readCallerView = async (res: Response) => {
    const body = (await res.json().catch(() => ({}))) as {
      detail?: string;
      code?: string;
    };
    return body;
  };

  it("a passed-through 401 (branch 2) is still readable downstream", async () => {
    recordRequests(() =>
      HttpResponse.json(
        { detail: "Invalid email or password", code: "auth.bad" },
        { status: 401 },
      ),
    );
    seedToken("at-live", 900);
    const res = await authedFetch("/v1/me/thing");
    expect(await readCallerView(res)).toEqual({
      detail: "Invalid email or password",
      code: "auth.bad",
    });
  });

  it("a 401 returned after an unavailable refresh (branch 4a) is still readable", async () => {
    countRefresh(() => new HttpResponse(null, { status: 503 }));
    recordRequests(() =>
      HttpResponse.json({ detail: "nope", code: "auth.bad" }, { status: 401 }),
    );
    seedExpiredToken("at-old");
    const res = await authedFetch("/v1/me/thing");
    expect(await readCallerView(res)).toEqual({
      detail: "nope",
      code: "auth.bad",
    });
  });

  it("a RETRIED 401 is still readable, terminal or not", async () => {
    countRefresh(() =>
      HttpResponse.json({ accessToken: "at-new", expiresIn: 900 }),
    );
    // Codeless first, terminal on the retry — otherwise branch 1 short-circuits
    // and the response the caller reads is the ORIGINAL, not the retried one.
    const rec = recordRequests((hit) =>
      hit === 1
        ? new HttpResponse(null, { status: 401 })
        : HttpResponse.json(
            { detail: "still no", code: "session_revoked" },
            { status: 401 },
          ),
    );
    seedExpiredToken("at-old");
    const res = await authedFetch("/v1/me/thing");
    expect(rec.seen.length).toBe(2);
    expect(await readCallerView(res)).toEqual({
      detail: "still no",
      code: "session_revoked",
    });
  });

  const unreadableBodies: [string, Response][] = [
    [
      "a non-JSON body",
      new HttpResponse("<html>gateway</html>", { status: 401 }),
    ],
    ["an empty body", new HttpResponse(null, { status: 401 })],
  ];
  it.each(unreadableBodies)(
    "%s is not terminal, does not throw, and stays readable",
    async (_l, response) => {
      recordRequests(() => response.clone());
      seedToken("at-live", 900);
      const res = await authedFetch("/v1/me/thing");
      expect(res.status).toBe(401);
      expect(getAccessToken()).toBe("at-live");
      await expect(res.text()).resolves.toBeDefined();
    },
  );
});

// The timing must be FORCED, never left to the scheduler: performRefresh
// clears inflightRefresh in a `finally` the moment the rotation resolves, so a
// 401 that comes back even slightly later finds no in-flight promise to join.
describe("a 401 answered after a sibling already rotated (§5.1, G8)", () => {
  it("takes branch 3: retries with the store's token and does NOT rotate again", async () => {
    const refresh = countRefresh(() =>
      HttpResponse.json({ accessToken: "at-new", expiresIn: 900 }),
    );
    let releaseB!: () => void;
    const bHeld = new Promise<void>((r) => {
      releaseB = r;
    });
    const seen: string[] = [];
    server.use(
      http.get(THING, async ({ request }) => {
        const which = request.headers.get("X-Which")!;
        const auth = request.headers.get("Authorization");
        seen.push(`${which}:${auth}`);
        if (which === "B" && auth === "Bearer at-old") await bHeld;
        return auth === "Bearer at-new"
          ? HttpResponse.json({ ok: true })
          : new HttpResponse(null, { status: 401 });
      }),
    );
    seedExpiredToken("at-old");

    const a = authedFetch("/v1/me/thing", { headers: { "X-Which": "A" } });
    const b = authedFetch("/v1/me/thing", { headers: { "X-Which": "B" } });
    // A completes its whole recovery first — including the rotation — and only
    // THEN is B's 401 released. "B's 401 comes back after the rotation" is a
    // fact of the test, not a hope.
    expect((await a).status).toBe(200);
    releaseB();
    const resB = await b;

    expect(resB.status).toBe(200); // NOT the stale 401
    expect(refresh.hits()).toBe(1); // NOT a second rotation
    expect(seen).toContain("B:Bearer at-new");
  });

  // Guards the ORDERING: a change-password rejection must not be replayed just
  // because a sibling rotated meanwhile. Branch 3 sits BEHIND the replay guard.
  // The sibling's token is installed from INSIDE the handler, so it is already
  // in the store when the 401 is classified — the only way to make the branch-3
  // condition true at the moment it is read.
  it("a live-token 401 in the same situation is still passed through", async () => {
    const refresh = countRefresh(() =>
      HttpResponse.json({ accessToken: "at-new", expiresIn: 900 }),
    );
    const rec = recordRequests(() => {
      // A sibling rotated while our request was in flight.
      setAccessToken("at-sibling", 900);
      return new HttpResponse(null, { status: 401 });
    });
    setSessionMarker();
    setAccessToken("at-live", 900);

    const res = await authedFetch("/v1/me/thing", {
      method: "POST",
      body: "{}",
    });
    expect(getAccessTokenSnapshot().token).toBe("at-sibling"); // the premise
    expect(res.status).toBe(401);
    expect(refresh.hits()).toBe(0);
    expect(rec.seen.length).toBe(1); // no replay with the sibling's token
  });

  // §4.3 branch 4a splits on the SENT bearer, not on the store, so a sign-out
  // that lands mid-flight does NOT veto the refresh: a bearer in memory when
  // the request left is proof a session existed. This is the case the brief's
  // "signed-out with no request" wording predates.
  it("a sign-out landing mid-flight still refreshes — the split is on the SENT bearer", async () => {
    const refresh = countRefresh(() =>
      HttpResponse.json({ accessToken: "at-new", expiresIn: 900 }),
    );
    const rec = recordRequests((hit) => {
      if (hit === 1) clearSessionLocally(); // a sign-out lands mid-flight
      return hit === 1
        ? new HttpResponse(null, { status: 401 })
        : HttpResponse.json({ ok: true });
    });
    seedExpiredToken("at-old");

    const res = await authedFetch("/v1/me/thing");
    // Branch 3 is skipped (the store's token is null, not a DIFFERENT token),
    // so branch 4a runs on the strength of the sent bearer alone.
    expect(res.status).toBe(200);
    expect(refresh.hits()).toBe(1);
    expect(rec.header(1, "Authorization")).toBe("Bearer at-new");
  });
});
