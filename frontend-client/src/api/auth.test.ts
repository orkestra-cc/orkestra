import { afterEach, describe, expect, it, vi } from "vitest";
import { http, HttpResponse } from "msw";

import {
  apiErrorCode,
  browserNavigation,
  fetchAuthPolicy,
  fetchOAuthProviders,
  initiateOAuthLogin,
  login,
  passwordLoginUsable,
} from "@/api/auth";
import { OAUTH_RETURN_TO_KEY } from "@/lib/oauthReturnTo";
import {
  clientPolicyHandler,
  openPolicy,
  providersHandler,
  providersUnavailableHandler,
  url,
} from "@/test/handlers";
import { server } from "@/test/server";

const POLICY = url("/v1/auth/client/policy");
const PROVIDERS = url("/v1/auth/client/providers");
const START = url("/v1/auth/client/oauth/login");
const LOGIN = url("/v1/auth/client/login");

describe("fetchAuthPolicy", () => {
  it("returns the wire fields, null included", async () => {
    server.use(clientPolicyHandler({ passwordLoginEnabled: null }));
    const policy = await fetchAuthPolicy();
    expect(policy.passwordLoginEnabled).toBeNull();
    expect(policy.passwordLoginBreakGlassEffective).toBe(false);
  });

  it("fills the PR 3 fields when an older backend omits them", async () => {
    server.use(
      http.get(POLICY, () =>
        HttpResponse.json({
          registrationEnabled: true,
          loginEnabled: false,
          passwordMinLength: 12,
        }),
      ),
    );
    await expect(fetchAuthPolicy()).resolves.toEqual({
      ...openPolicy,
      loginEnabled: false,
      passwordMinLength: 12,
    });
  });

  it("falls open on a 503 and on a network failure", async () => {
    server.use(
      http.get(POLICY, () =>
        HttpResponse.json({ code: "auth.policy_unavailable" }, { status: 503 }),
      ),
    );
    await expect(fetchAuthPolicy()).resolves.toEqual(openPolicy);
    server.use(http.get(POLICY, () => HttpResponse.error()));
    await expect(fetchAuthPolicy()).resolves.toEqual(openPolicy);
  });
});

describe("passwordLoginUsable", () => {
  it("is true only while the policy is unknown or explicitly true", () => {
    expect(passwordLoginUsable(undefined)).toBe(true);
    expect(
      passwordLoginUsable({ ...openPolicy, passwordLoginEnabled: true }),
    ).toBe(true);
    expect(
      passwordLoginUsable({ ...openPolicy, passwordLoginEnabled: false }),
    ).toBe(false);
    expect(
      passwordLoginUsable({ ...openPolicy, passwordLoginEnabled: null }),
    ).toBe(false);
  });
});

describe("apiErrorCode", () => {
  it("reads the stable backend code off this module's errors and nothing else", () => {
    const e = Object.assign(new Error("refused"), {
      status: 403,
      code: "auth.login_disabled",
    });
    expect(apiErrorCode(e)).toBe("auth.login_disabled");
    expect(apiErrorCode(new Error("plain"))).toBeUndefined();
    expect(apiErrorCode(null)).toBeUndefined();
    expect(apiErrorCode({ code: 42 })).toBeUndefined();
  });
});

describe("fetchOAuthProviders", () => {
  it("returns the allowlisted names in backend order and drops unknown ones with a warning", async () => {
    const warn = vi.spyOn(console, "warn").mockImplementation(() => {});
    server.use(providersHandler(["github", "facebook", "google"]));
    await expect(fetchOAuthProviders()).resolves.toEqual(["github", "google"]);
    expect(warn).toHaveBeenCalledTimes(1);
    expect(String(warn.mock.calls[0][0])).toContain("facebook");
    warn.mockRestore();
  });

  it("an empty list is the empty state", async () => {
    server.use(providersHandler([]));
    await expect(fetchOAuthProviders()).resolves.toEqual([]);
  });

  it.each([[{}], [{ providers: null }], [{ providers: "google" }], [[]]])(
    "a body whose providers is not an array (%j) is malformed and rejects — never the empty state",
    async (body) => {
      server.use(http.get(PROVIDERS, () => HttpResponse.json(body)));
      await expect(fetchOAuthProviders()).rejects.toMatchObject({
        status: 500,
      });
    },
  );

  it("rejects — never falls open — on 503 auth.policy_unavailable", async () => {
    server.use(providersUnavailableHandler());
    await expect(fetchOAuthProviders()).rejects.toMatchObject({
      status: 503,
      code: "auth.policy_unavailable",
    });
  });

  it("rejects on a network failure", async () => {
    server.use(http.get(PROVIDERS, () => HttpResponse.error()));
    await expect(fetchOAuthProviders()).rejects.toBeInstanceOf(Error);
  });

  it("sends credentials so the API host's cookies travel", async () => {
    let credentials: RequestCredentials | undefined;
    server.use(
      http.get(PROVIDERS, ({ request }) => {
        credentials = request.credentials;
        return HttpResponse.json({ providers: [] });
      }),
    );
    await fetchOAuthProviders();
    expect(credentials).toBe("include");
  });
});

describe("initiateOAuthLogin", () => {
  afterEach(() => vi.restoreAllMocks());

  it("POSTs {provider} with credentials, stashes the validated next, then leaves for authUrl", async () => {
    const assign = vi
      .spyOn(browserNavigation, "assign")
      .mockImplementation(() => {});
    const seen: Array<{ body: unknown; credentials: RequestCredentials }> = [];
    server.use(
      http.post(START, async ({ request }) => {
        seen.push({
          body: await request.json(),
          credentials: request.credentials,
        });
        return HttpResponse.json({
          authUrl: "https://idp.example/authorize?state=s1",
          state: "s1",
        });
      }),
    );
    await initiateOAuthLogin("google", "/account/security");
    expect(seen).toEqual([
      { body: { provider: "google" }, credentials: "include" },
    ]);
    expect(
      JSON.parse(sessionStorage.getItem(OAUTH_RETURN_TO_KEY)!).target,
    ).toBe("/account/security");
    expect(assign).toHaveBeenCalledWith(
      "https://idp.example/authorize?state=s1",
    );
  });

  it("stashes BEFORE assigning, and clears a stale record when next is unsafe", async () => {
    const order: string[] = [];
    vi.spyOn(browserNavigation, "assign").mockImplementation(() => {
      order.push(
        sessionStorage.getItem(OAUTH_RETURN_TO_KEY) === null
          ? "assign:empty"
          : "assign:stashed",
      );
    });
    server.use(
      http.post(START, () =>
        HttpResponse.json({ authUrl: "https://idp.example/a", state: "s" }),
      ),
    );
    await initiateOAuthLogin("github", "/account");
    expect(order).toEqual(["assign:stashed"]);

    sessionStorage.setItem(
      OAUTH_RETURN_TO_KEY,
      JSON.stringify({ target: "/account", createdAt: Date.now() }),
    );
    await initiateOAuthLogin("github", "//evil.example");
    expect(sessionStorage.getItem(OAUTH_RETURN_TO_KEY)).toBeNull();
    expect(order).toEqual(["assign:stashed", "assign:empty"]);
  });

  it.each([
    [403, "auth.oauth_provider_disabled"],
    [403, "auth.login_disabled"],
    [503, "auth.policy_unavailable"],
  ])(
    "surfaces %d %s without leaving the page or stashing",
    async (status, code) => {
      const assign = vi
        .spyOn(browserNavigation, "assign")
        .mockImplementation(() => {});
      server.use(
        http.post(START, () =>
          HttpResponse.json({ code, detail: "refused" }, { status }),
        ),
      );
      await expect(
        initiateOAuthLogin("apple", "/account"),
      ).rejects.toMatchObject({ status, code });
      expect(assign).not.toHaveBeenCalled();
      expect(sessionStorage.getItem(OAUTH_RETURN_TO_KEY)).toBeNull();
    },
  );

  it("treats a 200 without authUrl as a failure", async () => {
    const assign = vi
      .spyOn(browserNavigation, "assign")
      .mockImplementation(() => {});
    server.use(http.post(START, () => HttpResponse.json({ state: "s" })));
    await expect(initiateOAuthLogin("discord", null)).rejects.toMatchObject({
      status: 500,
    });
    expect(assign).not.toHaveBeenCalled();
  });
});

describe("login — the reported lifetime is never invented", () => {
  const credentials = { email: "a@b.c", password: "hunter22hunter22" };

  it("passes a reported expiresIn through verbatim", async () => {
    server.use(
      http.post(LOGIN, () =>
        HttpResponse.json({
          success: true,
          accessToken: "at-1",
          tokenType: "Bearer",
          expiresIn: 60,
        }),
      ),
    );
    await expect(login(credentials)).resolves.toEqual({
      kind: "token",
      accessToken: "at-1",
      tokenType: "Bearer",
      expiresIn: 60,
    });
  });

  // `expiresIn` is OPTIONAL on the wire, and defaulting it to 900 FABRICATES a
  // fifteen-minute lifetime the server never promised: on a deployment running
  // a 60s TTL the store would then read every 401 as "not a token problem" for
  // the rest of that quarter hour. toEqual ignores undefined properties on
  // BOTH sides, so this passes only while the field really is absent — a 900
  // would be an extra defined property and fail.
  it("leaves expiresIn UNDEFINED when the body omits it — never 900", async () => {
    server.use(
      http.post(LOGIN, () =>
        HttpResponse.json({ success: true, accessToken: "at-1" }),
      ),
    );
    await expect(login(credentials)).resolves.toEqual({
      kind: "token",
      accessToken: "at-1",
      tokenType: "Bearer",
    });
  });
});
