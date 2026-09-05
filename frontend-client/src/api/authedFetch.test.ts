import { afterEach, describe, expect, it, vi } from "vitest";
import { http, HttpResponse } from "msw";

import { authedFetch, PROACTIVE_REFRESH_SKEW_MS } from "@/api/authedFetch";
import { browserNavigation } from "@/api/auth";
import {
  clearSessionLocally,
  getAccessToken,
  getAccessTokenSnapshot,
  setAccessToken,
} from "@/auth/tokenStore";
import { hasSessionMarker, setSessionMarker } from "@/auth/sessionMarker";
import { url } from "@/test/handlers";
import { server } from "@/test/server";
// The module's own SOURCE TEXT, for the no-leak invariant at the bottom of this
// file. Vite's `?raw` gives the file verbatim, before any transform.
import authedFetchSource from "@/api/authedFetch.ts?raw";

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

// §4.11 migration. The proactive arm rotates BEFORE the request whenever the
// bearer it is about to send expires inside PROACTIVE_REFRESH_SKEW_MS — which
// every seedExpiredToken() fixture does, by construction. Answering that FIRST
// hit 503 makes the proactive attempt `unavailable`: token and marker survive,
// the seeded token goes out, and the case below exercises §4.3 exactly as it
// did before the arm existed — one /refresh-cookie hit higher. `respond` keeps
// its original hit numbering, so the fixture bodies are unchanged.
const proactiveUnavailableThen = (respond: (hit: number) => Response) =>
  countRefresh((hit) =>
    hit === 1 ? new HttpResponse(null, { status: 503 }) : respond(hit - 1),
  );

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

  // §4.2 rule 2, and the only enumerated rule of §4.2 with no other witness:
  // the httpOnly refresh cookie is Domain-scoped to the API host (ADR-0003
  // D-9) and attaches ONLY when credentials are explicitly included. Drop the
  // line and every other assertion in this file still passes while the whole
  // recovery quietly stops working in a browser, because the cookie never
  // travels. The default it falls back to is "same-origin", which is not it.
  it("sends credentials: include, and a caller cannot turn it off", async () => {
    const rec = recordRequests(() => HttpResponse.json({ ok: true }));
    seedToken("at-1", 900);
    await authedFetch("/v1/me/thing");
    expect(rec.seen[0].credentials).toBe("include");

    // Written AFTER the ...init spread, so it always wins — the same "last
    // write wins" rule the bearer follows.
    await authedFetch("/v1/me/thing", { credentials: "omit" });
    expect(rec.seen[1].credentials).toBe("include");
  });

  it("sends no Authorization at all when the store is empty", async () => {
    const rec = recordRequests(() => HttpResponse.json({ ok: true }));
    // no token, no marker
    await authedFetch("/v1/me/thing");
    expect(rec.header(0, "Authorization")).toBeNull();
  });
});

describe("authedFetch 401 recovery (§4.3)", () => {
  it("expired token → 401 → refresh → retry carries the NEW bearer", async () => {
    const refresh = proactiveUnavailableThen(() =>
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
    expect(refresh.hits()).toBe(2); // the failed proactive attempt, then the rotation
    expect(rec.seen.length).toBe(2);
    expect(rec.header(0, "Authorization")).toBe("Bearer at-old");
    expect(rec.header(1, "Authorization")).toBe("Bearer at-new");
  });

  // Neither of these is visible from a header or a status code, so both are
  // invisible to every other case in this file. A retry that dropped
  // `init.body` reaches the handler as an EMPTY POST — the request succeeds,
  // the caller gets a 200, and the payload is silently gone. And §5.10: the
  // retry state is a local variable, never a header; v0.10.0's deleted
  // client.ts middleware set `X-Retry: 1` and leaked the recovery to the
  // server, where nothing needed it.
  it("the retry re-sends the caller's body and adds no X-Retry header", async () => {
    const refresh = proactiveUnavailableThen(() =>
      HttpResponse.json({ accessToken: "at-new", expiresIn: 900 }),
    );
    const rec = recordRequests((hit) =>
      hit === 1
        ? new HttpResponse(null, { status: 401 })
        : HttpResponse.json({ ok: true }),
    );
    seedExpiredToken("at-old");

    const res = await authedFetch("/v1/me/thing", {
      method: "POST",
      body: JSON.stringify({ a: 1 }),
    });
    expect(res.status).toBe(200);
    expect(refresh.hits()).toBe(2); // proactive 503, then the rotation
    expect(rec.seen.length).toBe(2);
    expect(await rec.seen[0].text()).toBe('{"a":1}');
    expect(await rec.seen[1].text()).toBe('{"a":1}');
    expect(rec.header(0, "X-Retry")).toBeNull();
    expect(rec.header(1, "X-Retry")).toBeNull();
  });

  // THE lockout-hazard regression test (§4.4). change-password is an
  // AUTHENTICATED endpoint that answers 401 when the CURRENT PASSWORD IN THE
  // BODY is wrong; a blanket "401 → refresh → retry" re-sends the failed
  // attempt and the backend counts it again, so a user who mistypes twice is
  // locked out as though they had tried four times.
  //
  // The fixture's remaining life IS the test: a token with 20 minutes left
  // passes against the broken implementation too. It used to be 20s — the
  // value the removed 30s SKEW would have mis-classified as "not live" — and
  // §4.11 is why it no longer can be: 20s is INSIDE
  // PROACTIVE_REFRESH_SKEW_MS, so the arm would rotate before the request and
  // this case would stop exercising branch 2 on a live bearer at all. 300s is
  // outside the proactive window and still inside any plausible margin the 401
  // comparison might grow. "Alive, so the handler ran" was always the
  // load-bearing part, never the exact number.
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
    seedToken("at-live", 300);
    // The fixture asserts its own premise: 300s of life, which the server
    // still accepts, so the handler DID run and counted the failed attempt —
    // and which is outside the proactive window, so nothing rotated first.
    const { expiresAt } = getAccessTokenSnapshot();
    expect(expiresAt! - Date.now()).toBeGreaterThan(290_000);
    expect(expiresAt! - Date.now()).toBeLessThan(310_000);

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
  //
  // Both halves are inside PROACTIVE_REFRESH_SKEW_MS by construction, so each
  // now costs a proactive rotation of its own. The case keeps its shape by
  // counting the two kinds APART: /refresh-cookie answers 503 on the ODD hits
  // (the proactive attempts, which must change nothing) and 200 on the even
  // one. The property is unchanged and now reads as a difference — the
  // boundary token earns a REACTIVE rotation, the +1ms token earns none.
  it("expiresAt === sentAt counts as expired; sentAt + 1 counts as live", async () => {
    vi.useFakeTimers();
    try {
      const refresh = countRefresh((hit) =>
        hit % 2 === 1
          ? new HttpResponse(null, { status: 503 })
          : HttpResponse.json({ accessToken: "at-new", expiresIn: 900 }),
      );
      recordRequests((hit) =>
        hit % 2 === 1
          ? new HttpResponse(null, { status: 401 })
          : HttpResponse.json({ ok: true }),
      );
      setSessionMarker();

      setAccessToken("at-boundary", 0); // expiresAt === Date.now()
      await authedFetch("/v1/me/thing");
      // Its proactive attempt (503), then the 401-driven rotation.
      expect(refresh.hits()).toBe(2);

      setAccessToken("at-live", 0.001); // expiresAt === Date.now() + 1ms
      await authedFetch("/v1/me/thing");
      // One more proactive attempt, and NO second rotation — passed through.
      expect(refresh.hits()).toBe(3);
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
    const refresh = proactiveUnavailableThen(() =>
      HttpResponse.json({ code: "refresh_rotation_raced" }, { status: 409 }),
    );
    recordRequests(() => new HttpResponse(null, { status: 401 }));
    seedExpiredToken("at-old");

    const res = await authedFetch("/v1/me/thing");
    expect(res.status).toBe(401);
    expect(refresh.hits()).toBe(3); // the proactive 503, then 409 TWICE
    expect(getAccessToken()).toBe("at-old");
    expect(hasSessionMarker()).toBe(true);
  });

  it("refresh 401 → token and marker cleared so AuthProvider can re-render (G3)", async () => {
    const refresh = proactiveUnavailableThen(
      () => new HttpResponse(null, { status: 401 }),
    );
    recordRequests(() => new HttpResponse(null, { status: 401 }));
    seedExpiredToken("at-old");

    const res = await authedFetch("/v1/me/thing");
    expect(res.status).toBe(401);
    // The proactive 503 AND the reactive rotation. Without this the case
    // still passes if someone drops the proactiveUnavailableThen wrapper —
    // a bare 401 responder clears the token on the PROACTIVE arm, and the
    // reactive §4.3 path this case exists to cover is never exercised.
    expect(refresh.hits()).toBe(2);
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

  it("no bearer and no marker (anonymous) → ZERO refresh requests (branch 2 passes it through; 4b is unreachable)", async () => {
    const refresh = countRefresh(() => HttpResponse.json({ accessToken: "x" }));
    recordRequests(() => new HttpResponse(null, { status: 401 }));
    // no token, no marker

    const res = await authedFetch("/v1/me/thing");
    expect(res.status).toBe(401);
    expect(refresh.hits()).toBe(0);
  });

  // With the proactive attempt answered 503 the three concurrent calls
  // coalesce TWICE — one proactive rotation shared by the burst, then one
  // reactive rotation shared by the three 401s. The coalescing this case
  // exists to pin is now pinned twice over.
  it("a burst of three 401s produces exactly one reactive /refresh-cookie", async () => {
    const refresh = proactiveUnavailableThen(() =>
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
    expect(refresh.hits()).toBe(2); // one proactive, one reactive — not five
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
    const refresh = proactiveUnavailableThen(() =>
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
    expect(refresh.hits()).toBe(2); // the proactive 503, then ONE rotation
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
    const refresh = proactiveUnavailableThen(() =>
      HttpResponse.json({ accessToken: "at-new", expiresIn: -1 }),
    );
    const rec = recordRequests(() => new HttpResponse(null, { status: 401 }));
    seedExpiredToken("at-old");

    const res = await authedFetch("/v1/me/thing");
    expect(res.status).toBe(401);
    expect(refresh.hits()).toBe(2); // the proactive 503 + ONE rotation, no more
    expect(rec.seen.length).toBe(2); // original + exactly ONE retry
    expect(rec.header(1, "Authorization")).toBe("Bearer at-new");
  });

  // Guards against "any retry 401 means signed out", which would sign out the
  // §4.4 mirror case for mistyping a password.
  it("a retry that 401s with NO code changes nothing", async () => {
    const refresh = proactiveUnavailableThen(() =>
      HttpResponse.json({ accessToken: "at-new", expiresIn: 900 }),
    );
    const rec = recordRequests(() => new HttpResponse(null, { status: 401 }));
    seedExpiredToken("at-old");

    const res = await authedFetch("/v1/me/thing");
    expect(res.status).toBe(401);
    expect(rec.seen.length).toBe(2);
    expect(refresh.hits()).toBe(2); // the proactive 503, then never a SECOND rotation
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
    proactiveUnavailableThen(() =>
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
    const refresh = proactiveUnavailableThen(() =>
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
    expect(refresh.hits()).toBe(2); // one proactive 503 + one rotation — B added neither
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
    const refresh = proactiveUnavailableThen(() =>
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
    expect(refresh.hits()).toBe(2); // the proactive 503, then the rotation
    expect(rec.header(1, "Authorization")).toBe("Bearer at-new");
  });
});

// §4.11. The arm sits BEFORE the request, so it is the one piece of this file
// that costs no 401 and needs no proof: there is no request to replay. Its
// whole contract is "rotate when the bearer we are about to send expires
// inside PROACTIVE_REFRESH_SKEW_MS, re-snapshot, send whatever the store then
// holds" — and, negatively, that the skew never reaches the comparison below.
describe("authedFetch proactive rotation (§4.11)", () => {
  it("near expiry: one /refresh-cookie BEFORE the request, which carries the NEW bearer", async () => {
    const order: string[] = [];
    const refresh = countRefresh(() => {
      order.push("refresh");
      return HttpResponse.json({ accessToken: "at-new", expiresIn: 900 });
    });
    const rec = recordRequests(() => {
      order.push("thing");
      return HttpResponse.json({ ok: true });
    });
    seedToken("at-near", 20);

    const res = await authedFetch("/v1/me/thing");
    expect(res.status).toBe(200);
    expect(refresh.hits()).toBe(1);
    // The ordering IS the feature. A count alone would also pass against a
    // reactive-only implementation that happened to rotate afterwards.
    expect(order).toEqual(["refresh", "thing"]);
    // Zero 401s, which is the whole point: no request is spent discovering an
    // expiry the client already knew about.
    expect(rec.seen.length).toBe(1);
    expect(rec.header(0, "Authorization")).toBe("Bearer at-new");
  });

  it("far from expiry: no rotation, the seeded bearer goes out", async () => {
    const refresh = countRefresh(() =>
      HttpResponse.json({ accessToken: "at-new", expiresIn: 900 }),
    );
    const rec = recordRequests(() => HttpResponse.json({ ok: true }));
    seedToken("at-1", 900);

    const res = await authedFetch("/v1/me/thing");
    expect(res.status).toBe(200);
    expect(refresh.hits()).toBe(0);
    expect(rec.header(0, "Authorization")).toBe("Bearer at-1");
  });

  // An UNKNOWN expiry counts as LIVE here for the same reason it does in
  // branch 2: rotating on "we cannot tell" would rotate on every request made
  // with a token whose lifetime was never learned — the refresh loop the D3
  // bound exists to prevent, arrived at from the other side.
  it("an UNKNOWN expiry does not rotate", async () => {
    const refresh = countRefresh(() => HttpResponse.json({ accessToken: "x" }));
    const rec = recordRequests(() => HttpResponse.json({ ok: true }));
    setSessionMarker();
    setAccessToken("opaque-not-a-jwt"); // no duration, unreadable exp
    expect(getAccessTokenSnapshot().expiresAt).toBeNull();

    await authedFetch("/v1/me/thing");
    expect(refresh.hits()).toBe(0);
    expect(rec.header(0, "Authorization")).toBe("Bearer opaque-not-a-jwt");
  });

  // The marker is deliberately STAMPED here: with no marker refreshAccessToken
  // would short-circuit anyway, and the case would pass without the
  // `sent.token !== null` guard ever being read.
  it("no bearer at all: no rotation is even attempted", async () => {
    const refresh = countRefresh(() => HttpResponse.json({ accessToken: "x" }));
    const rec = recordRequests(() => HttpResponse.json({ ok: true }));
    setSessionMarker(); // …but the store is empty

    const res = await authedFetch("/v1/me/thing");
    expect(res.status).toBe(200);
    expect(refresh.hits()).toBe(0);
    expect(rec.header(0, "Authorization")).toBeNull();
  });

  // `unavailable`: token and marker untouched (§4.1's allowlist), so the
  // request goes out with the OLD bearer and §4.3 owns whatever 401 follows.
  // A failed proactive rotation costs one round-trip and changes nothing else.
  it("a proactive rotation that is `unavailable` sends the OLD bearer and changes nothing", async () => {
    const refresh = countRefresh(() => new HttpResponse(null, { status: 503 }));
    const rec = recordRequests(() => HttpResponse.json({ ok: true }));
    seedToken("at-near", 20);

    const res = await authedFetch("/v1/me/thing");
    expect(res.status).toBe(200);
    expect(refresh.hits()).toBe(1);
    expect(rec.seen.length).toBe(1);
    expect(rec.header(0, "Authorization")).toBe("Bearer at-near");
    expect(getAccessToken()).toBe("at-near");
    expect(hasSessionMarker()).toBe(true);
  });

  // The case that proves §4.11 cannot STRAND the 401 path: the rotation is
  // attempted, fails, the dead bearer goes out anyway and branch 2's proof (2)
  // recovers it exactly as it does without the arm.
  it("already expired at send: a failed proactive attempt still leaves proof (2) to recover", async () => {
    const order: string[] = [];
    const refresh = countRefresh((hit) => {
      order.push(`refresh#${hit}`);
      return hit === 1
        ? new HttpResponse(null, { status: 503 })
        : HttpResponse.json({ accessToken: "at-new", expiresIn: 900 });
    });
    const rec = recordRequests((hit) => {
      order.push(`thing#${hit}`);
      return hit === 1
        ? new HttpResponse(null, { status: 401 })
        : HttpResponse.json({ ok: true });
    });
    seedExpiredToken("at-old");

    const res = await authedFetch("/v1/me/thing");
    expect(res.status).toBe(200);
    expect(order).toEqual(["refresh#1", "thing#1", "refresh#2", "thing#2"]);
    expect(refresh.hits()).toBe(2);
    expect(rec.header(0, "Authorization")).toBe("Bearer at-old");
    expect(rec.header(1, "Authorization")).toBe("Bearer at-new");
  });

  it("the skew stays strictly below the backend's MinAccessTokenTTL (ADR-0020 D3)", () => {
    // 60_000 ms is MinAccessTokenTTL
    // (backend/internal/core/auth/services/auth_duration_bounds.go:30). At or
    // above that floor a token minted at the minimum TTL is ALREADY inside the
    // window the moment it arrives, so every request would rotate again — a
    // refresh loop.
    expect(PROACTIVE_REFRESH_SKEW_MS).toBeLessThan(60_000);
  });

  // The behavioural twin of the bound above, and the one that actually fails
  // if the constant is raised: a floor-length token keeps half its life quiet.
  it("does not loop on a token minted at the backend minimum TTL (60s)", async () => {
    const refresh = countRefresh(() =>
      HttpResponse.json({ accessToken: "at-new", expiresIn: 60 }),
    );
    const rec = recordRequests(() => HttpResponse.json({ ok: true }));
    seedToken("at-floor", 60);

    await authedFetch("/v1/me/thing");
    await authedFetch("/v1/me/thing");
    expect(refresh.hits()).toBe(0);
    expect(rec.header(0, "Authorization")).toBe("Bearer at-floor");
    expect(rec.header(1, "Authorization")).toBe("Bearer at-floor");
  });

  // THE no-leak invariant, and it is asserted against the module's own SOURCE
  // because a behavioural test cannot express it: the proactive predicate and
  // branch 2's predicate agree on almost every input and differ only where the
  // difference is the bug (a token with 20s of life is still accepted by the
  // server, so the handler DID run — the round-11 replay hole). Two constants,
  // two predicates, one file.
  it("PROACTIVE_REFRESH_SKEW_MS never appears below the 401 line (§4.11 invariant)", () => {
    // Comments are stripped so the prose above branch 2 — which discusses the
    // skew at length — cannot make this test pass or fail.
    const code = authedFetchSource
      .replace(/\/\*[\s\S]*?\*\//g, "")
      .replace(/\/\/.*$/gm, "");
    const cut = code.indexOf("if (res.status !== 401) return res;");
    expect(cut).toBeGreaterThan(-1);

    const before = code.slice(0, cut);
    const after = code.slice(cut);
    expect(after).not.toContain("PROACTIVE_REFRESH_SKEW_MS");
    // Exactly two uses above the line: the declaration and the pre-send check.
    expect(before.match(/PROACTIVE_REFRESH_SKEW_MS/g)).toHaveLength(2);
    expect(before).toContain(
      "export const PROACTIVE_REFRESH_SKEW_MS = 30_000;",
    );
    expect(before).toMatch(
      /sent\.expiresAt - Date\.now\(\) < PROACTIVE_REFRESH_SKEW_MS/,
    );
    // …and the margin-free comparison is byte-identical to what it was.
    expect(after).toContain(
      "sent.expiresAt !== null && sent.expiresAt <= sentAt",
    );
  });
});

// The client SPA has no step-up modal and is deliberately not growing one
// (spec §4.2 D14). reauthentication_required is therefore the one 401 code
// this wrapper answers with a NAVIGATION: everything else it can either
// recover from or hand back to the caller.
describe("authedFetch reauthentication_required (§4.2 D14, branch 1b)", () => {
  afterEach(() => {
    window.history.pushState({}, "", "/");
  });

  it("clears the session and leaves for /login with a sanitised next", async () => {
    // Imported from @/api/auth on purpose: the seam moved to its own module to
    // break an import cycle, and this asserts the re-export still hands out
    // the same object the wrapper calls.
    const assign = vi
      .spyOn(browserNavigation, "assign")
      .mockImplementation(() => {});
    window.history.pushState({}, "", "/account/security/mfa");
    const refresh = countRefresh(() => HttpResponse.json({ accessToken: "x" }));
    const rec = recordRequests(() =>
      HttpResponse.json(
        { code: "reauthentication_required", maxAgeSeconds: 300, authTime: 0 },
        { status: 401 },
      ),
    );
    seedToken("at-live", 900);

    const res = await authedFetch("/v1/me/thing");

    expect(res.status).toBe(401);
    expect(assign).toHaveBeenCalledWith(
      "/login?next=%2Faccount%2Fsecurity%2Fmfa",
    );
    // BOTH halves. The marker is what makes the cold load after this
    // navigation attempt a silent refresh, and a token minted from the same
    // cookie carries the same stale auth_time — the user would land back on
    // the refusal with nothing to do about it.
    expect(getAccessToken()).toBeNull();
    expect(hasSessionMarker()).toBe(false);
    // No rotation and no replay: neither ages a session backwards.
    expect(refresh.hits()).toBe(0);
    expect(rec.seen.length).toBe(1);
  });

  // sanitizeNext is the SPA's single open-redirect gate, and it also refuses
  // the auth routes — a next pointing at /login would loop the user back into
  // the page they were sent to.
  it.each([
    ["an encoded protocol-relative leader", "/%2fevil.example.com/steal"],
    ["the login route itself", "/login"],
  ])("falls back to a bare /login for %s", async (_label, path) => {
    const assign = vi
      .spyOn(browserNavigation, "assign")
      .mockImplementation(() => {});
    window.history.pushState({}, "", path);
    recordRequests(() =>
      HttpResponse.json({ code: "reauthentication_required" }, { status: 401 }),
    );
    seedToken("at-live", 900);

    await authedFetch("/v1/me/thing");

    expect(assign).toHaveBeenCalledWith("/login");
  });

  // The sibling gate answer keeps its existing behaviour: the page renders it
  // as inline error copy, and nothing navigates. An enrolled client user
  // REPLACING a factor gets this code, and MfaEnrolPage keeps them off that
  // path by reading /me/mfa first.
  it("does not navigate on step_up_required", async () => {
    const assign = vi
      .spyOn(browserNavigation, "assign")
      .mockImplementation(() => {});
    window.history.pushState({}, "", "/account/security/mfa");
    recordRequests(() =>
      HttpResponse.json({ code: "step_up_required" }, { status: 401 }),
    );
    seedToken("at-live", 900);

    const res = await authedFetch("/v1/me/thing");

    expect(res.status).toBe(401);
    expect(assign).not.toHaveBeenCalled();
    expect(getAccessToken()).toBe("at-live");
    expect(hasSessionMarker()).toBe(true);
  });
});
