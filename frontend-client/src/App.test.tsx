import { beforeEach, describe, expect, it } from "vitest";
import { http, HttpResponse } from "msw";
import { useLocation, type InitialEntry } from "react-router";
import { useEffect } from "react";
import { screen, waitFor } from "@testing-library/react";

import { App } from "@/App";
import { hasSessionMarker, setSessionMarker } from "@/auth/sessionMarker";
import {
  clientPolicyHandler,
  meHandler,
  providersHandler,
  url,
} from "@/test/handlers";
import { renderWithProviders } from "@/test/render";
import { server } from "@/test/server";

const REFRESH = url("/v1/auth/client/refresh-cookie");
const MFA_STATUS = url("/v1/auth/client/me/mfa");
const tokenBody = { accessToken: "at-1", tokenType: "Bearer", expiresIn: 900 };

// Every location the app COMMITTED, in order. "the login page never
// rendered" has to be a claim about the whole run rather than about the
// last frame: LoginPage now forwards an already-authenticated visitor to
// its ?next=, so a final-state assertion alone would stay green even with
// the guard redirecting on the first frame again.
const visited: string[] = [];

beforeEach(() => {
  visited.length = 0;
});

// Reads the router location from OUTSIDE <Routes>: App owns the route table,
// this probe only reports where the app is.
const LocationProbe = () => {
  const location = useLocation();
  const here = location.pathname + location.search + location.hash;
  useEffect(() => {
    visited.push(here);
  }, [here]);
  return <div data-testid="app-location">{here}</div>;
};

const renderAppAt = (entry: InitialEntry) =>
  renderWithProviders(
    <>
      <App />
      <LocationProbe />
    </>,
    { routerEntries: [entry] },
  );

const renderApp = (search: string) =>
  renderAppAt({ pathname: "/auth/callback", search });

describe("App — the OAuth callback through the real route table and Layout (F15)", () => {
  it("success: Layout's anonymous header, scrub before the refresh, then AccountPage authenticated", async () => {
    let release!: () => void;
    const gate = new Promise<void>((resolve) => {
      release = resolve;
    });
    let refreshHits = 0;
    let locationAtRequest: string | null = null;
    server.use(
      clientPolicyHandler(), // Layout mounts /policy while anonymous
      meHandler(), // Layout + AccountPage once authenticated
      http.post(REFRESH, async () => {
        refreshHits++;
        locationAtRequest =
          screen.queryByTestId("app-location")?.textContent ?? null;
        await gate;
        return HttpResponse.json(tokenBody);
      }),
    );
    renderApp("?success=true&provider=google");

    // The shell is up around the callback page, still anonymous.
    expect(
      await screen.findByRole("link", { name: "Sign in" }),
    ).toBeInTheDocument();
    expect(await screen.findByText(/completing sign-in/i)).toBeInTheDocument();
    // Scrubbed before the refresh left: the URL bar already shows the bare path.
    await waitFor(() =>
      expect(screen.getByTestId("app-location")).toHaveTextContent(
        /^\/auth\/callback$/,
      ),
    );
    await waitFor(() => expect(refreshHits).toBe(1));
    // Order proof on the real tree: the bare path was committed before the
    // refresh request left.
    expect(locationAtRequest).toBe("/auth/callback");
    expect(screen.getByTestId("app-location")).toHaveTextContent(
      /^\/auth\/callback$/,
    );

    release();
    // AccountPage (RequireAuth-guarded) with the /me profile, and the header
    // switched to the authenticated nav.
    expect(
      await screen.findByRole("heading", { name: "Account" }),
    ).toBeInTheDocument();
    // AccountPage prints the profile name twice — the header block and
    // the "Name" field — so findByText's single-match contract cannot
    // hold here. findAllByText still fails if the /me profile never lands.
    expect(await screen.findAllByText("Client User")).not.toHaveLength(0);
    expect(screen.getByTestId("app-location")).toHaveTextContent(/^\/account$/);
    expect(screen.queryByRole("link", { name: "Sign in" })).toBeNull();
    expect(hasSessionMarker()).toBe(true);
  });

  it("failure: the mapped copy renders inside the shell, on a clean URL, with no refresh", async () => {
    let refreshHits = 0;
    server.use(
      clientPolicyHandler(),
      http.post(REFRESH, () => {
        refreshHits++;
        return HttpResponse.json(tokenBody);
      }),
    );
    renderApp("?success=false&error=oauth_access_denied");
    expect(
      await screen.findByText(/cancelled at the identity provider/i),
    ).toBeInTheDocument();
    expect(
      await screen.findByRole("link", { name: "Sign in" }),
    ).toBeInTheDocument();
    await waitFor(() =>
      expect(screen.getByTestId("app-location")).toHaveTextContent(
        /^\/auth\/callback$/,
      ),
    );
    expect(refreshHits).toBe(0);
  });
});

// §8 #11 — the cold-load window. RequireAuth used to redirect on the FIRST
// render, when `token` is still null because AuthProvider's mount refresh
// has not even left, so every reload / deep link / bookmark of a protected
// route bounced a signed-in user to a login form under a signed-in header.
// Entered through the real route table because that is where the bug lives:
// the previous two cases enter at /auth/callback, the one route immune to it
// (OAuthCallbackPage awaits the bootstrap before it navigates).
describe("App — a cold load of a protected route waits for the session bootstrap (§8 #11)", () => {
  it("with a valid refresh cookie the requested route renders and /login is never visited", async () => {
    // A returning user: the marker is what makes the mount refresh fire at all.
    setSessionMarker();
    let refreshHits = 0;
    server.use(
      clientPolicyHandler(), // Layout's policy read, fired while still anonymous
      meHandler(), // Layout's avatar + nav once the token lands
      http.get(MFA_STATUS, () =>
        HttpResponse.json({
          status: "not_required",
          backupCodesRemaining: 0,
          requiresMfa: false,
          webauthnCredentials: 0,
        }),
      ),
      http.post(REFRESH, () => {
        refreshHits++;
        return HttpResponse.json(tokenBody);
      }),
    );
    renderAppAt("/account/security");

    expect(
      await screen.findByRole("heading", { name: "Security" }),
    ).toBeInTheDocument();
    expect(screen.getByTestId("app-location")).toHaveTextContent(
      /^\/account\/security$/,
    );
    await waitFor(() => expect(refreshHits).toBe(1));
    // /login is the only route that mounts LoginPage, so never having been
    // there is exactly "the login form never rendered".
    expect(visited.filter((p) => p.startsWith("/login"))).toEqual([]);
    expect(screen.queryByLabelText(/^email$/i)).toBeNull();
  });

  it("anonymous: the guard still redirects, with the requested path on ?next=", async () => {
    let refreshHits = 0;
    server.use(
      clientPolicyHandler(),
      providersHandler([]),
      http.post(REFRESH, () => {
        refreshHits++;
        return HttpResponse.json(tokenBody);
      }),
    );
    renderAppAt("/account/security");

    // The login form, not a blank shell: the bootstrap settles for a
    // marker-less visitor without a request being made.
    expect(await screen.findByLabelText(/^email$/i)).toBeInTheDocument();
    expect(screen.getByTestId("app-location")).toHaveTextContent(
      "/login?next=%2Faccount%2Fsecurity",
    );
    expect(refreshHits).toBe(0);
  });
});
