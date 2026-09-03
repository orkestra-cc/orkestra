import { describe, expect, it } from "vitest";
import { http, HttpResponse } from "msw";
import { screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";

import { hasSessionMarker, setSessionMarker } from "@/auth/sessionMarker";
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

// §8 #11 — the readiness flag RequireAuth waits on. `token === null` means
// two different things on a cold load ("signed out" and "not decided yet")
// and the guard cannot tell them apart without this.
const LOGOUT = url("/v1/auth/client/logout");
const bootToken = {
  accessToken: "at-boot",
  tokenType: "Bearer",
  expiresIn: 900,
};

const BootProbe = () => {
  const { isBootstrapping, isAuthenticated, signIn, signOut } = useAuth();
  return (
    <>
      <div data-testid="boot">
        {`${isBootstrapping ? "bootstrapping" : "settled"}:${
          isAuthenticated ? "in" : "out"
        }`}
      </div>
      <button type="button" onClick={() => signIn("at-manual", 900)}>
        sign in
      </button>
      <button type="button" onClick={() => void signOut()}>
        sign out
      </button>
    </>
  );
};

describe("AuthProvider — the mount bootstrap flag (§8 #11)", () => {
  it("stays bootstrapping until the mount refresh answers, then settles authenticated", async () => {
    setSessionMarker();
    let release!: () => void;
    const gate = new Promise<void>((resolve) => {
      release = resolve;
    });
    server.use(
      http.post(REFRESH, async () => {
        await gate;
        return HttpResponse.json(bootToken);
      }),
    );
    renderWithProviders(<BootProbe />);
    // Held open by the gate, so this is the state of the flag and not a
    // race the assertion happened to win.
    expect(screen.getByTestId("boot")).toHaveTextContent("bootstrapping:out");
    release();
    await waitFor(() =>
      expect(screen.getByTestId("boot")).toHaveTextContent("settled:in"),
    );
  });

  it("settles for a marker-less visitor, whose refresh never leaves", async () => {
    let calls = 0;
    server.use(
      http.post(REFRESH, () => {
        calls++;
        return HttpResponse.json(bootToken);
      }),
    );
    renderWithProviders(<BootProbe />);
    await waitFor(() =>
      expect(screen.getByTestId("boot")).toHaveTextContent("settled:out"),
    );
    expect(calls).toBe(0);
  });

  it("settles signed-out when the cookie is rejected", async () => {
    setSessionMarker();
    server.use(
      http.post(REFRESH, () =>
        HttpResponse.json({ detail: "no session" }, { status: 401 }),
      ),
    );
    renderWithProviders(<BootProbe />);
    await waitFor(() =>
      expect(screen.getByTestId("boot")).toHaveTextContent("settled:out"),
    );
    expect(hasSessionMarker()).toBe(false);
  });

  it("signIn and signOut leave the settled flag alone", async () => {
    server.use(
      http.post(LOGOUT, () => new HttpResponse(null, { status: 204 })),
    );
    renderWithProviders(<BootProbe />);
    await waitFor(() =>
      expect(screen.getByTestId("boot")).toHaveTextContent("settled:out"),
    );
    const user = userEvent.setup();
    await user.click(screen.getByRole("button", { name: "sign in" }));
    expect(screen.getByTestId("boot")).toHaveTextContent("settled:in");
    await user.click(screen.getByRole("button", { name: "sign out" }));
    await waitFor(() =>
      expect(screen.getByTestId("boot")).toHaveTextContent("settled:out"),
    );
  });
});
