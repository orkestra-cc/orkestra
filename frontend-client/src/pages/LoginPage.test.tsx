import { afterEach, describe, expect, it, vi } from "vitest";
import { http, HttpResponse, type JsonBodyType } from "msw";
import { Route, Routes, useLocation } from "react-router";
import { screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";

import { browserNavigation } from "@/api/auth";
import { RequireAuth } from "@/auth/RequireAuth";
import { OAUTH_RETURN_TO_KEY } from "@/lib/oauthReturnTo";
import { LoginPage } from "@/pages/LoginPage";
import {
  clientPolicyHandler,
  openPolicy,
  providersHandler,
  url,
} from "@/test/handlers";
import { renderWithProviders, waitForQuerySettled } from "@/test/render";
import { server } from "@/test/server";

const Probe = ({ label }: { label: string }) => {
  const location = useLocation();
  return (
    <div data-testid={`${label}-location`}>
      {location.pathname + location.search}
    </div>
  );
};

const renderLogin = (entry = "/login") =>
  renderWithProviders(
    <Routes>
      <Route path="/login" element={<LoginPage />} />
      <Route
        path="/account"
        element={
          <RequireAuth>
            <Probe label="account" />
          </RequireAuth>
        }
      />
      <Route
        path="/account/security"
        element={
          <RequireAuth>
            <Probe label="deeplink" />
          </RequireAuth>
        }
      />
    </Routes>,
    { routerEntries: [entry] },
  );

// A GET the test releases by hand, to observe the page while that query is
// still in flight.
const deferredJson = (path: string, body: JsonBodyType) => {
  let release!: () => void;
  const gate = new Promise<void>((resolve) => {
    release = resolve;
  });
  server.use(
    http.get(url(path), async () => {
      await gate;
      return HttpResponse.json(body);
    }),
  );
  return release;
};

const emailField = () => screen.queryByLabelText(/^email$/i);
const START = url("/v1/auth/client/oauth/login");
const LOGIN = url("/v1/auth/client/login");
const PROVIDERS = url("/v1/auth/client/providers");
const tokenBody = {
  success: true,
  accessToken: "at-1",
  tokenType: "Bearer",
  expiresIn: 900,
};

// Increment B makes the page query /providers on mount; every test stubs
// it from the start (an empty list is neutral for increment A's cases) so
// no case trips MSW's unhandled-request error once B lands.

describe("LoginPage — policy gate and password-off (spec §4.10, D1)", () => {
  it("paints no password form until /policy has resolved", async () => {
    const releasePolicy = deferredJson("/v1/auth/client/policy", openPolicy);
    server.use(providersHandler([]));
    renderLogin();
    // en.json "loading": "Loading…" (the providers' own loading copy differs).
    expect(await screen.findByText(/^loading…$/i)).toBeInTheDocument();
    expect(emailField()).toBeNull();
    releasePolicy();
    expect(await screen.findByLabelText(/^email$/i)).toBeInTheDocument();
  });

  it("password on: form and its two links", async () => {
    server.use(clientPolicyHandler(), providersHandler([]));
    renderLogin();
    expect(await screen.findByLabelText(/^email$/i)).toBeInTheDocument();
    expect(
      screen.getByRole("link", { name: /forgot password/i }),
    ).toBeInTheDocument();
    expect(
      screen.getByRole("link", { name: /create an account/i }),
    ).toBeInTheDocument();
    // login.subtitle, not the SSO subtitle.
    expect(
      screen.getByText(/enter your credentials to continue/i),
    ).toBeInTheDocument();
  });

  it.each([false, null])(
    "passwordLoginEnabled=%s hides the form and its links and switches the subtitle",
    async (value) => {
      server.use(
        clientPolicyHandler({ passwordLoginEnabled: value }),
        providersHandler([]),
      );
      renderLogin();
      // login.subtitleSso renders only once the policy landed and said "off".
      expect(
        await screen.findByText(
          /continue with one of the sign-in providers below/i,
        ),
      ).toBeInTheDocument();
      expect(emailField()).toBeNull();
      expect(screen.queryByLabelText(/^password$/i)).toBeNull();
      expect(
        screen.queryByRole("link", { name: /forgot password/i }),
      ).toBeNull();
      expect(
        screen.queryByRole("link", { name: /create an account/i }),
      ).toBeNull();
    },
  );

  it("kill switch: banner + disabled password submit", async () => {
    server.use(
      clientPolicyHandler({ loginEnabled: false }),
      providersHandler([]),
    );
    renderLogin();
    expect(
      await screen.findByText(/login is temporarily disabled/i),
    ).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /^sign in$/i })).toBeDisabled();
  });
});

describe("LoginPage — password path keeps one redirect policy (D4)", () => {
  const signInWithPassword = async () => {
    const user = userEvent.setup();
    await user.type(await screen.findByLabelText(/^email$/i), "a@b.c");
    await user.type(screen.getByLabelText(/^password$/i), "hunter22hunter22");
    await user.click(screen.getByRole("button", { name: /^sign in$/i }));
  };

  it("lands on a safe ?next=", async () => {
    server.use(
      clientPolicyHandler(),
      providersHandler([]),
      http.post(LOGIN, () => HttpResponse.json(tokenBody)),
    );
    renderLogin("/login?next=%2Faccount%2Fsecurity");
    await signInWithPassword();
    expect(await screen.findByTestId("deeplink-location")).toHaveTextContent(
      "/account/security",
    );
  });

  it("falls back to /account on an unsafe ?next=", async () => {
    server.use(
      clientPolicyHandler(),
      providersHandler([]),
      http.post(LOGIN, () => HttpResponse.json(tokenBody)),
    );
    renderLogin("/login?next=https%3A%2F%2Fevil.example%2Fx");
    await signInWithPassword();
    expect(await screen.findByTestId("account-location")).toHaveTextContent(
      "/account",
    );
  });

  // Regression guard — GREEN before the change too: the MFA hand-off must
  // survive the extraction of MfaChallenge.
  it("still hands a partial login to MfaChallenge and completes through it", async () => {
    server.use(
      clientPolicyHandler(),
      providersHandler([]),
      http.post(LOGIN, () =>
        HttpResponse.json({
          success: true,
          requiresMfa: true,
          mfaToken: "ch-9",
          webauthnAvailable: false,
        }),
      ),
      http.post(
        url("/v1/auth/client/mfa/login/verify"),
        async ({ request }) => {
          const body = (await request.json()) as { challengeId: string };
          return body.challengeId === "ch-9"
            ? HttpResponse.json({ ...tokenBody, sessionId: "s1" })
            : HttpResponse.json({ detail: "wrong challenge" }, { status: 401 });
        },
      ),
    );
    renderLogin();
    await signInWithPassword();
    // login.mfa.prompt, en.json.
    expect(
      await screen.findByText(/enter the 6-digit code/i),
    ).toBeInTheDocument();
    const user = userEvent.setup();
    await user.type(screen.getByLabelText(/verification code/i), "123456");
    await user.click(screen.getByRole("button", { name: /^verify$/i }));
    expect(await screen.findByTestId("account-location")).toHaveTextContent(
      "/account",
    );
  });
});

describe("LoginPage — provider section (spec §4.10 loading / error / empty / list)", () => {
  it("paints no provider button until /policy has resolved either", async () => {
    const releasePolicy = deferredJson("/v1/auth/client/policy", openPolicy);
    server.use(providersHandler(["google"]));
    renderLogin();
    expect(await screen.findByText(/^loading…$/i)).toBeInTheDocument();
    expect(
      screen.queryByRole("button", { name: "Continue with Google" }),
    ).toBeNull();
    releasePolicy();
    expect(
      await screen.findByRole("button", { name: "Continue with Google" }),
    ).toBeInTheDocument();
  });

  it("password on + providers: divider and one button per provider", async () => {
    server.use(clientPolicyHandler(), providersHandler(["google", "github"]));
    renderLogin();
    expect(
      await screen.findByRole("button", { name: "Continue with Google" }),
    ).toBeInTheDocument();
    expect(
      screen.getByRole("button", { name: "Continue with GitHub" }),
    ).toBeInTheDocument();
    expect(screen.getByText(/or continue with/i)).toBeInTheDocument();
    expect(screen.queryByText(/no sign-in method/i)).toBeNull();
  });

  it("password off + providers: buttons only, no divider, no alert", async () => {
    server.use(
      clientPolicyHandler({ passwordLoginEnabled: false }),
      providersHandler(["google"]),
    );
    renderLogin();
    expect(
      await screen.findByRole("button", { name: "Continue with Google" }),
    ).toBeInTheDocument();
    expect(emailField()).toBeNull();
    expect(screen.queryByText(/or continue with/i)).toBeNull();
    expect(screen.queryByText(/no sign-in method/i)).toBeNull();
  });

  it("password off + providers resolved empty → the no-method alert", async () => {
    server.use(
      clientPolicyHandler({ passwordLoginEnabled: false }),
      providersHandler([]),
    );
    renderLogin();
    expect(await screen.findByRole("alert")).toHaveTextContent(
      /no sign-in method is currently available/i,
    );
    expect(emailField()).toBeNull();
  });

  it("password off + providers 503 → the retryable error, never the alert; retry recovers", async () => {
    let calls = 0;
    server.use(
      clientPolicyHandler({ passwordLoginEnabled: false }),
      http.get(PROVIDERS, () => {
        calls++;
        return calls === 1
          ? HttpResponse.json(
              { code: "auth.policy_unavailable" },
              { status: 503 },
            )
          : HttpResponse.json({ providers: ["google"] });
      }),
    );
    renderLogin();
    expect(
      await screen.findByText(/could not load the sign-in providers/i),
    ).toBeInTheDocument();
    expect(screen.queryByText(/no sign-in method/i)).toBeNull();
    await userEvent
      .setup()
      .click(screen.getByRole("button", { name: /try again/i }));
    expect(
      await screen.findByRole("button", { name: "Continue with Google" }),
    ).toBeInTheDocument();
    expect(
      screen.queryByText(/could not load the sign-in providers/i),
    ).toBeNull();
    expect(calls).toBe(2);
  });

  it("password off + a malformed providers body → the retryable error, never the alert (B3)", async () => {
    server.use(
      clientPolicyHandler({ passwordLoginEnabled: false }),
      http.get(PROVIDERS, () => HttpResponse.json({})),
    );
    renderLogin();
    expect(
      await screen.findByText(/could not load the sign-in providers/i),
    ).toBeInTheDocument();
    expect(screen.queryByText(/no sign-in method/i)).toBeNull();
    expect(
      screen.getByRole("button", { name: /try again/i }),
    ).toBeInTheDocument();
  });

  it("password on + zero providers → the plain form, no section, no divider, no alert", async () => {
    server.use(clientPolicyHandler(), providersHandler([]));
    const { queryClient } = renderLogin();
    expect(await screen.findByLabelText(/^email$/i)).toBeInTheDocument();
    // The tree is identical before and after the providers query when the
    // list is empty and the method is on — anchor on the cache entry.
    await waitForQuerySettled(queryClient, ["oauthProviders"]);
    expect(screen.queryByText(/or continue with/i)).toBeNull();
    expect(screen.queryByText(/loading sign-in options/i)).toBeNull();
    expect(screen.queryByText(/no sign-in method/i)).toBeNull();
  });

  it("shows the providers loading state distinctly, then the buttons", async () => {
    server.use(clientPolicyHandler());
    const releaseProviders = deferredJson("/v1/auth/client/providers", {
      providers: ["discord"],
    });
    renderLogin();
    expect(
      await screen.findByText(/loading sign-in options/i),
    ).toBeInTheDocument();
    releaseProviders();
    expect(
      await screen.findByRole("button", { name: "Continue with Discord" }),
    ).toBeInTheDocument();
    expect(screen.queryByText(/loading sign-in options/i)).toBeNull();
  });

  it("kill switch hides the provider section", async () => {
    server.use(
      clientPolicyHandler({ loginEnabled: false }),
      providersHandler(["google"]),
    );
    const { queryClient } = renderLogin();
    expect(
      await screen.findByText(/login is temporarily disabled/i),
    ).toBeInTheDocument();
    await waitForQuerySettled(queryClient, ["oauthProviders"]);
    expect(
      screen.queryByRole("button", { name: "Continue with Google" }),
    ).toBeNull();
    expect(screen.queryByText(/no sign-in method/i)).toBeNull();
  });
});

describe("LoginPage — OAuth start (spec §4.10)", () => {
  afterEach(() => vi.restoreAllMocks());

  it("starts the flow with the allowlisted provider, stashes the validated next and leaves", async () => {
    const assign = vi
      .spyOn(browserNavigation, "assign")
      .mockImplementation(() => {});
    const bodies: unknown[] = [];
    server.use(
      clientPolicyHandler(),
      providersHandler(["google", "github"]),
      http.post(START, async ({ request }) => {
        bodies.push(await request.json());
        return HttpResponse.json({
          authUrl: "https://idp.example/authorize",
          state: "s",
        });
      }),
    );
    renderLogin("/login?next=%2Faccount%2Fsecurity");
    await userEvent
      .setup()
      .click(
        await screen.findByRole("button", { name: "Continue with GitHub" }),
      );
    await waitFor(() =>
      expect(assign).toHaveBeenCalledWith("https://idp.example/authorize"),
    );
    expect(bodies).toEqual([{ provider: "github" }]);
    expect(
      JSON.parse(sessionStorage.getItem(OAUTH_RETURN_TO_KEY)!).target,
    ).toBe("/account/security");
    expect(
      screen.getByRole("button", { name: /redirecting to github/i }),
    ).toBeDisabled();
    expect(
      screen.getByRole("button", { name: "Continue with Google" }),
    ).toBeDisabled();
  });

  it("an unsafe ?next= is not stashed (and a stale record is dropped)", async () => {
    vi.spyOn(browserNavigation, "assign").mockImplementation(() => {});
    server.use(
      clientPolicyHandler(),
      providersHandler(["google"]),
      http.post(START, () =>
        HttpResponse.json({ authUrl: "https://idp.example/a", state: "s" }),
      ),
    );
    sessionStorage.setItem(
      OAUTH_RETURN_TO_KEY,
      JSON.stringify({ target: "/account", createdAt: Date.now() }),
    );
    renderLogin("/login?next=%2F%2Fevil.example");
    await userEvent
      .setup()
      .click(
        await screen.findByRole("button", { name: "Continue with Google" }),
      );
    await waitFor(() =>
      expect(sessionStorage.getItem(OAUTH_RETURN_TO_KEY)).toBeNull(),
    );
  });

  it("maps a 403 auth.oauth_provider_disabled to copy, re-enables the buttons, never renders the detail", async () => {
    const assign = vi
      .spyOn(browserNavigation, "assign")
      .mockImplementation(() => {});
    server.use(
      clientPolicyHandler(),
      providersHandler(["apple"]),
      http.post(START, () =>
        HttpResponse.json(
          {
            code: "auth.oauth_provider_disabled",
            detail: "refused-by-backend",
          },
          { status: 403 },
        ),
      ),
    );
    renderLogin();
    await userEvent
      .setup()
      .click(
        await screen.findByRole("button", { name: "Continue with Apple" }),
      );
    expect(
      await screen.findByText(
        /apple sign-in is not available on this surface/i,
      ),
    ).toBeInTheDocument();
    expect(screen.queryByText(/refused-by-backend/)).toBeNull();
    expect(
      screen.getByRole("button", { name: "Continue with Apple" }),
    ).toBeEnabled();
    expect(assign).not.toHaveBeenCalled();
  });

  it("maps a 503 auth.policy_unavailable to the shared retryable copy", async () => {
    vi.spyOn(browserNavigation, "assign").mockImplementation(() => {});
    server.use(
      clientPolicyHandler(),
      providersHandler(["google"]),
      http.post(START, () =>
        HttpResponse.json({ code: "auth.policy_unavailable" }, { status: 503 }),
      ),
    );
    renderLogin();
    await userEvent
      .setup()
      .click(
        await screen.findByRole("button", { name: "Continue with Google" }),
      );
    expect(
      await screen.findByText(/sign-in policy is temporarily unavailable/i),
    ).toBeInTheDocument();
  });
});
