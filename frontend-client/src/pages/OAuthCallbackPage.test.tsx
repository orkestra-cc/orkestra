import { describe, expect, it } from "vitest";
import { http, HttpResponse } from "msw";
import { Route, Routes, useLocation } from "react-router";
import { screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";

import { RequireAuth } from "@/auth/RequireAuth";
import { hasSessionMarker, setSessionMarker } from "@/auth/sessionMarker";
import { getAccessTokenSnapshot } from "@/auth/tokenStore";
import {
  OAUTH_RETURN_TO_KEY,
  OAUTH_RETURN_TO_TTL_MS,
} from "@/lib/oauthReturnTo";
import { OAuthCallbackPage } from "@/pages/OAuthCallbackPage";
import { url } from "@/test/handlers";
import { renderWithProviders } from "@/test/render";
import { server } from "@/test/server";

const REFRESH = url("/v1/auth/client/refresh-cookie");
const VERIFY = url("/v1/auth/client/mfa/login/verify");
const tokenBody = { accessToken: "at-1", tokenType: "Bearer", expiresIn: 900 };
const GENERIC = /^sign-in failed\. please try again\.$/i;

const Probe = ({ label }: { label: string }) => {
  const location = useLocation();
  return (
    <>
      <div data-testid={`${label}-location`}>
        {location.pathname + location.search + location.hash}
      </div>
      <div data-testid={`${label}-state`}>{JSON.stringify(location.state)}</div>
    </>
  );
};

// Every value in web storage, joined — the assertion surface for "no
// access token or challenge id is ever persisted".
const storageDump = (): string =>
  [
    ...Array.from(
      { length: localStorage.length },
      (_, i) => localStorage.getItem(localStorage.key(i)!) ?? "",
    ),
    ...Array.from(
      { length: sessionStorage.length },
      (_, i) => sessionStorage.getItem(sessionStorage.key(i)!) ?? "",
    ),
  ].join("\n");

// A refresh endpoint the test releases by hand. At the moment the request
// arrives it records the marker AND the router location the page had
// committed (read from the Probe's DOM) — the order proof for "scrub before
// the first request": both must already hold when the handler runs, not
// merely become true at some point.
const deferredRefresh = () => {
  let release!: () => void;
  const gate = new Promise<void>((resolve) => {
    release = resolve;
  });
  let hits = 0;
  let markerAtRequest: boolean | null = null;
  let locationAtRequest: string | null = null;
  server.use(
    http.post(REFRESH, async () => {
      hits++;
      markerAtRequest = hasSessionMarker();
      locationAtRequest =
        screen.queryByTestId("cb-location")?.textContent ?? null;
      await gate;
      return HttpResponse.json(tokenBody);
    }),
  );
  return {
    release,
    hits: () => hits,
    markerAtRequest: () => markerAtRequest,
    locationAtRequest: () => locationAtRequest,
  };
};

// A refresh endpoint whose first answer is `first`, then a token.
const refreshFailingOnce = (first: () => Response) => {
  let calls = 0;
  server.use(
    http.post(REFRESH, () => {
      calls++;
      return calls === 1 ? first() : HttpResponse.json(tokenBody);
    }),
  );
  return () => calls;
};

const renderCallback = (search: string, hash = "") =>
  renderWithProviders(
    <Routes>
      <Route
        path="/auth/callback"
        element={
          <>
            <OAuthCallbackPage />
            <Probe label="cb" />
          </>
        }
      />
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
      <Route path="/login" element={<Probe label="login" />} />
    </Routes>,
    { routerEntries: [{ pathname: "/auth/callback", search, hash }] },
  );

const stash = (target: string, createdAt = Date.now()) =>
  sessionStorage.setItem(
    OAUTH_RETURN_TO_KEY,
    JSON.stringify({ target, createdAt }),
  );

describe("OAuthCallbackPage — success", () => {
  it("scrubs the URL before the refresh request, stamps the marker first, then lands authenticated on /account", async () => {
    const refresh = deferredRefresh();
    renderCallback("?success=true&provider=google");
    await waitFor(() =>
      expect(screen.getByTestId("cb-location")).toHaveTextContent(
        /^\/auth\/callback$/,
      ),
    );
    await waitFor(() => expect(refresh.hits()).toBe(1));
    // Order, not coincidence: when the request left, the page had already
    // committed the bare path and the marker was already stamped.
    expect(refresh.locationAtRequest()).toBe("/auth/callback");
    expect(refresh.markerAtRequest()).toBe(true);
    // Nothing navigates while the bootstrap pends, and the URL stays clean.
    expect(screen.getByTestId("cb-location")).toHaveTextContent(
      /^\/auth\/callback$/,
    );
    expect(screen.queryByTestId("account-location")).toBeNull();
    refresh.release();
    expect(await screen.findByTestId("account-location")).toHaveTextContent(
      "/account",
    );
    expect(screen.queryByTestId("login-location")).toBeNull();
    expect(storageDump()).not.toContain("at-1");
  });

  it("honours a fresh stashed return target and deletes it in the first effect", async () => {
    stash("/account/security");
    const refresh = deferredRefresh();
    renderCallback("?success=true&provider=github");
    // Taken in the first effect — already gone once render returns.
    expect(sessionStorage.getItem(OAUTH_RETURN_TO_KEY)).toBeNull();
    refresh.release();
    expect(await screen.findByTestId("deeplink-location")).toHaveTextContent(
      "/account/security",
    );
  });

  it("ignores a stale stashed return target", async () => {
    stash("/account/security", Date.now() - OAUTH_RETURN_TO_TTL_MS - 1);
    const refresh = deferredRefresh();
    renderCallback("?success=true&provider=github");
    refresh.release();
    expect(await screen.findByTestId("account-location")).toHaveTextContent(
      "/account",
    );
  });

  it("treats a signed-out bootstrap as a login error, never a protected route, and clears the marker", async () => {
    server.use(
      http.post(REFRESH, () =>
        HttpResponse.json({ detail: "no session" }, { status: 401 }),
      ),
    );
    renderCallback("?success=true&provider=google");
    expect(
      await screen.findByText(/no session could be established/i),
    ).toBeInTheDocument();
    expect(
      screen.getByRole("link", { name: /back to sign in/i }),
    ).toHaveAttribute("href", "/login");
    expect(screen.queryByTestId("account-location")).toBeNull();
    expect(hasSessionMarker()).toBe(false);
  });

  it("keeps the bootstrap state and offers retry when the refresh endpoint answers 503", async () => {
    const calls = refreshFailingOnce(() =>
      HttpResponse.json(
        { code: "session_enforcement_unavailable" },
        { status: 503 },
      ),
    );
    renderCallback("?success=true&provider=google");
    const retry = await screen.findByRole("button", { name: /try again/i });
    expect(hasSessionMarker()).toBe(true);
    expect(screen.queryByTestId("account-location")).toBeNull();
    await userEvent.setup().click(retry);
    expect(await screen.findByTestId("account-location")).toHaveTextContent(
      "/account",
    );
    expect(calls()).toBe(2);
  });

  it("a network failure is the same retryable state — never a stuck spinner (B2)", async () => {
    const calls = refreshFailingOnce(() => HttpResponse.error());
    renderCallback("?success=true&provider=google");
    const retry = await screen.findByRole("button", { name: /try again/i });
    expect(screen.queryByText(/completing sign-in/i)).toBeNull();
    expect(hasSessionMarker()).toBe(true);
    await userEvent.setup().click(retry);
    expect(await screen.findByTestId("account-location")).toHaveTextContent(
      "/account",
    );
    expect(calls()).toBe(2);
  });

  it("with a session marker already present (returning user), the page's first effect runs before the provider's automatic refresh leaves, and the flow still lands on the deep link", async () => {
    // AuthProvider fires refreshAccessToken on mount whenever the marker
    // exists (AuthProvider.tsx:35-42) — a request this page did not issue.
    // React runs a child's passive effects before its ancestor's, so the
    // page's first effect — take-and-delete the return target, then
    // navigate(replace), synchronously — has run before that request
    // leaves. The stash is the observable: gone at request time. (The
    // Probe's DOM may lag React's re-render here, so it is not the anchor
    // in this case; the coalescing itself is proven in tokenStore.test.ts.)
    setSessionMarker();
    stash("/account/security");
    let hits = 0;
    let stashAtRequest: string | null = "not-yet-observed";
    let release!: () => void;
    const gate = new Promise<void>((resolve) => {
      release = resolve;
    });
    server.use(
      http.post(REFRESH, async () => {
        hits++;
        stashAtRequest = sessionStorage.getItem(OAUTH_RETURN_TO_KEY);
        await gate;
        return HttpResponse.json(tokenBody);
      }),
    );
    renderCallback("?success=true&provider=google");
    await waitFor(() => expect(hits).toBeGreaterThanOrEqual(1));
    expect(stashAtRequest).toBeNull();
    release();
    expect(await screen.findByTestId("deeplink-location")).toHaveTextContent(
      /^\/account\/security$/,
    );
    expect(hasSessionMarker()).toBe(true);
    expect(storageDump()).not.toContain("at-1");
  });
});

describe("OAuthCallbackPage — failures (closed contract)", () => {
  it("takes the return target on an error outcome too", async () => {
    stash("/account/security");
    renderCallback("?success=false&error=oauth_access_denied");
    expect(
      await screen.findByText(/cancelled at the identity provider/i),
    ).toBeInTheDocument();
    expect(sessionStorage.getItem(OAUTH_RETURN_TO_KEY)).toBeNull();
  });

  it("renders the mapped copy for an allowlisted code, never the raw code, on a clean URL", async () => {
    renderCallback("?success=false&error=oauth_signup_disabled");
    expect(await screen.findByText(/invitation-only/i)).toBeInTheDocument();
    expect(screen.queryByText(/oauth_signup_disabled/)).toBeNull();
    await waitFor(() =>
      expect(screen.getByTestId("cb-location")).toHaveTextContent(
        /^\/auth\/callback$/,
      ),
    );
    expect(
      screen.getByRole("link", { name: /back to sign in/i }),
    ).toHaveAttribute("href", "/login");
  });

  it("collapses an unknown code to the generic copy and never renders raw URL text", async () => {
    renderCallback("?success=false&error=%3Cscript%3Ealert(1)%3C%2Fscript%3E");
    expect(await screen.findByText(GENERIC)).toBeInTheDocument();
    expect(document.body.textContent).not.toContain("<script>");
    expect(document.body.textContent).not.toContain("alert(1)");
  });

  it("treats an ambiguous payload (MFA fragment + query outcome) as the generic failure", async () => {
    renderCallback(
      "?success=true&provider=google",
      "#requiresMfa=true&mfaToken=ch-1&webauthnAvailable=false",
    );
    expect(await screen.findByText(GENERIC)).toBeInTheDocument();
    expect(screen.queryByText(/enter the 6-digit code/i)).toBeNull();
    expect(screen.queryByTestId("account-location")).toBeNull();
    expect(storageDump()).not.toContain("ch-1");
  });

  it("does not issue a refresh on a failure outcome", async () => {
    let hits = 0;
    server.use(
      http.post(REFRESH, () => {
        hits++;
        return HttpResponse.json(tokenBody);
      }),
    );
    renderCallback("?success=false&error=oauth_login_failed");
    expect(await screen.findByText(GENERIC)).toBeInTheDocument();
    expect(hits).toBe(0);
    expect(hasSessionMarker()).toBe(false);
  });
});

describe("OAuthCallbackPage — MFA continuation", () => {
  it("renders MfaChallenge from the fragment with a clean URL, no router state and nothing in storage, then completes", async () => {
    server.use(
      http.post(VERIFY, async ({ request }) => {
        const body = (await request.json()) as { challengeId: string };
        return body.challengeId === "ch-1"
          ? HttpResponse.json({
              success: true,
              ...tokenBody,
              accessToken: "at-2",
              sessionId: "s1",
            })
          : HttpResponse.json({ detail: "wrong challenge" }, { status: 401 });
      }),
    );
    renderCallback(
      "",
      "#requiresMfa=true&mfaToken=ch-1&webauthnAvailable=true",
    );
    // login.mfa.prompt — the same MfaChallenge the password path renders.
    expect(
      await screen.findByText(/enter the 6-digit code/i),
    ).toBeInTheDocument();
    expect(screen.getByTestId("cb-location")).toHaveTextContent(
      /^\/auth\/callback$/,
    );
    // The challenge lives in component memory only: no router state.
    expect(screen.getByTestId("cb-state")).toHaveTextContent(/^null$/);
    expect(storageDump()).not.toContain("ch-1");

    const user = userEvent.setup();
    await user.type(screen.getByLabelText(/verification code/i), "123456");
    await user.click(screen.getByRole("button", { name: /^verify$/i }));
    expect(await screen.findByTestId("account-location")).toHaveTextContent(
      "/account",
    );
    expect(hasSessionMarker()).toBe(true);
    expect(storageDump()).not.toContain("at-2");
    expect(storageDump()).not.toContain("ch-1");
  });

  it("honours a fresh return target after the MFA completion", async () => {
    stash("/account/security");
    server.use(
      http.post(VERIFY, () =>
        HttpResponse.json({ success: true, ...tokenBody, sessionId: "s1" }),
      ),
    );
    renderCallback(
      "",
      "#requiresMfa=true&mfaToken=ch-1&webauthnAvailable=false",
    );
    const user = userEvent.setup();
    await user.type(
      await screen.findByLabelText(/verification code/i),
      "123456",
    );
    await user.click(screen.getByRole("button", { name: /^verify$/i }));
    expect(await screen.findByTestId("deeplink-location")).toHaveTextContent(
      "/account/security",
    );
  });

  // §4.6 — the mirror of the login page's MFA case. This page has the OTHER
  // `signIn` call site, and a lifetime dropped here fails just as silently.
  it("records the token lifetime the MFA verify response carried (§4.6)", async () => {
    server.use(
      http.post(VERIFY, () =>
        HttpResponse.json({
          success: true,
          accessToken: "at-mfa",
          tokenType: "Bearer",
          expiresIn: 180,
          sessionId: "s1",
        }),
      ),
    );
    // The store stamps Date.now() + lifetime at install, so the recorded
    // expiry must fall inside [before, after] + 180s — a window milliseconds
    // wide. That excludes null (the lifetime was dropped) and 900.
    const before = Date.now();
    renderCallback(
      "",
      "#requiresMfa=true&mfaToken=ch-1&webauthnAvailable=false",
    );
    const user = userEvent.setup();
    await user.type(
      await screen.findByLabelText(/verification code/i),
      "123456",
    );
    await user.click(screen.getByRole("button", { name: /^verify$/i }));
    await waitFor(() => expect(getAccessTokenSnapshot().token).toBe("at-mfa"));
    const after = Date.now();
    const { expiresAt } = getAccessTokenSnapshot();
    expect(expiresAt).not.toBeNull();
    expect(expiresAt!).toBeGreaterThanOrEqual(before + 180_000);
    expect(expiresAt!).toBeLessThanOrEqual(after + 180_000);
  });

  it("cancel returns to the login page", async () => {
    renderCallback(
      "",
      "#requiresMfa=true&mfaToken=ch-1&webauthnAvailable=false",
    );
    await userEvent
      .setup()
      .click(await screen.findByRole("button", { name: /cancel/i }));
    expect(await screen.findByTestId("login-location")).toHaveTextContent(
      "/login",
    );
  });
});
