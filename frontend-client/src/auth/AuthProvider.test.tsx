import { describe, expect, it } from "vitest";
import { http, HttpResponse } from "msw";
import { screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";

import { hasSessionMarker } from "@/auth/sessionMarker";
import { useAuth } from "@/auth/useAuth";
import { url } from "@/test/handlers";
import { renderWithProviders } from "@/test/render";
import { server } from "@/test/server";

const REFRESH = url("/v1/auth/client/refresh-cookie");

const Probe = () => {
  const { isAuthenticated, bootstrapFromRefreshCookie } = useAuth();
  return (
    <button type="button" onClick={() => void bootstrapFromRefreshCookie()}>
      {isAuthenticated ? "in" : "out"}
    </button>
  );
};

describe("AuthProvider.bootstrapFromRefreshCookie", () => {
  it("flips the context to authenticated once the refresh cookie yields a token", async () => {
    server.use(
      http.post(REFRESH, () =>
        HttpResponse.json({
          accessToken: "at-9",
          tokenType: "Bearer",
          expiresIn: 900,
        }),
      ),
    );
    renderWithProviders(<Probe />);
    expect(screen.getByRole("button")).toHaveTextContent("out");
    expect(hasSessionMarker()).toBe(false);
    await userEvent.setup().click(screen.getByRole("button"));
    await waitFor(() =>
      expect(screen.getByRole("button")).toHaveTextContent("in"),
    );
    expect(hasSessionMarker()).toBe(true);
  });

  it("stays signed out and leaves no marker when the cookie is rejected", async () => {
    let calls = 0;
    server.use(
      http.post(REFRESH, () => {
        calls++;
        return HttpResponse.json({ detail: "no session" }, { status: 401 });
      }),
    );
    renderWithProviders(<Probe />);
    await userEvent.setup().click(screen.getByRole("button"));
    // Anchor on the request having completed — the marker is false both
    // before the click and after the 401, so its value alone is no anchor.
    await waitFor(() => expect(calls).toBe(1));
    expect(hasSessionMarker()).toBe(false);
    expect(screen.getByRole("button")).toHaveTextContent("out");
  });
});
