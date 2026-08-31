import { describe, expect, it } from "vitest";
import { http, HttpResponse } from "msw";
import { useLocation } from "react-router";
import { screen, waitFor } from "@testing-library/react";

import { App } from "@/App";
import { hasSessionMarker } from "@/auth/sessionMarker";
import { clientPolicyHandler, meHandler, url } from "@/test/handlers";
import { renderWithProviders } from "@/test/render";
import { server } from "@/test/server";

const REFRESH = url("/v1/auth/client/refresh-cookie");
const tokenBody = { accessToken: "at-1", tokenType: "Bearer", expiresIn: 900 };

// Reads the router location from OUTSIDE <Routes>: App owns the route table,
// this probe only reports where the app is.
const LocationProbe = () => {
  const location = useLocation();
  return (
    <div data-testid="app-location">
      {location.pathname + location.search + location.hash}
    </div>
  );
};

const renderApp = (search: string) =>
  renderWithProviders(
    <>
      <App />
      <LocationProbe />
    </>,
    { routerEntries: [{ pathname: "/auth/callback", search }] },
  );

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
    // Scrubbed before the refresh left, while Layout's own query is in flight.
    await waitFor(() =>
      expect(screen.getByTestId("app-location")).toHaveTextContent(
        /^\/auth\/callback$/,
      ),
    );
    await waitFor(() => expect(refreshHits).toBe(1));
    // Order proof on the real tree: the bare path was committed before the
    // refresh left, with Layout's own query in flight beside it.
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
