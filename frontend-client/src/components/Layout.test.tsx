import { describe, expect, it } from "vitest";
import { http, HttpResponse } from "msw";
import { Route, Routes } from "react-router";
import { screen, waitFor } from "@testing-library/react";

import { Layout } from "@/components/Layout";
import { setSessionMarker } from "@/auth/sessionMarker";
import { meHandler, openPolicy, url } from "@/test/handlers";
import { renderWithProviders, waitForQuerySettled } from "@/test/render";
import { server } from "@/test/server";

const POLICY = url("/v1/auth/client/policy");
const REFRESH = url("/v1/auth/client/refresh-cookie");

const renderLayout = () =>
  renderWithProviders(
    <Routes>
      <Route element={<Layout />}>
        <Route path="/" element={<div>home</div>} />
      </Route>
    </Routes>,
  );

// The policy answer is held behind a gate the test releases, so "the CTA is
// present before /policy lands" is a fact of the test rather than a race
// against MSW: the bootstrap now sits in front of the query, and a
// first-paint assertion would otherwise be timing-dependent.
const gatedPolicy = (overrides: Partial<typeof openPolicy> = {}) => {
  let release!: () => void;
  const gate = new Promise<void>((resolve) => {
    release = resolve;
  });
  server.use(
    http.get(POLICY, async () => {
      await gate;
      return HttpResponse.json({ ...openPolicy, ...overrides });
    }),
  );
  return () => release();
};

describe("Layout — anonymous header CTA (spec §4.10)", () => {
  it("hides the Sign-up CTA once the policy says password sign-in is off", async () => {
    const release = gatedPolicy({ passwordLoginEnabled: false });
    renderLayout();
    // The auth slot is empty until the bootstrap settles (§8 #18b), then it
    // falls open (CTA present) while /policy is still in flight.
    expect(screen.queryByRole("link", { name: "Sign up" })).toBeNull();
    expect(
      await screen.findByRole("link", { name: "Sign up" }),
    ).toBeInTheDocument();

    release();
    await waitFor(() =>
      expect(
        screen.queryByRole("link", { name: "Sign up" }),
      ).not.toBeInTheDocument(),
    );
    expect(screen.getByRole("link", { name: "Sign in" })).toBeInTheDocument();
  });

  it("keeps the CTA when the method is on", async () => {
    const release = gatedPolicy();
    const { queryClient } = renderLayout();
    release();
    await waitForQuerySettled(queryClient, ["authPolicy"]);
    expect(screen.getByRole("link", { name: "Sign up" })).toBeInTheDocument();
  });

  // Regression guard — GREEN before the change.
  it("still hides the CTA when self-service registration is off", async () => {
    const release = gatedPolicy({ registrationEnabled: false });
    renderLayout();
    expect(
      await screen.findByRole("link", { name: "Sign up" }),
    ).toBeInTheDocument();

    release();
    await waitFor(() =>
      expect(
        screen.queryByRole("link", { name: "Sign up" }),
      ).not.toBeInTheDocument(),
    );
  });
});

// §8 #18(b). `isAuthenticated` is `token !== null`, which is false for the
// WHOLE cold-load window — so judging the header on it alone shows a returning
// user "Sign in / Sign up" until the refresh answers, and fires an
// anonymous-only /policy request that is pure waste for them. It is the #11
// defect in the header rather than in the route guard, and it has the same
// cure: wait for `isBootstrapping`.
describe("Layout — the bootstrap window (§8 #18b)", () => {
  it("a cold load with a valid cookie never paints the anonymous header, and never fires /policy", async () => {
    let release!: () => void;
    const gate = new Promise<void>((resolve) => {
      release = resolve;
    });
    let policyHits = 0;
    let refreshHits = 0;
    server.use(
      http.get(POLICY, () => {
        policyHits++;
        return HttpResponse.json(openPolicy);
      }),
      meHandler(),
      http.post(REFRESH, async () => {
        refreshHits++;
        await gate;
        return HttpResponse.json({
          accessToken: "at-1",
          tokenType: "Bearer",
          expiresIn: 900,
        });
      }),
    );
    // A returning user: the refresh cookie is in the jar and the marker says
    // so, but the in-memory token store starts empty on every document.
    setSessionMarker();

    renderLayout();
    // First paint, and then the whole in-flight window: NOTHING in the auth
    // slot. Not a spinner — the window is one round-trip and a spinner in a
    // header reads as breakage.
    expect(screen.queryByRole("link", { name: "Sign in" })).toBeNull();
    expect(screen.queryByRole("link", { name: "Sign up" })).toBeNull();
    await waitFor(() => expect(refreshHits).toBe(1));
    expect(screen.queryByRole("link", { name: "Sign in" })).toBeNull();
    expect(screen.queryByRole("link", { name: "Sign up" })).toBeNull();
    // The rest of the header stays put, so nothing shifts when the real
    // controls appear.
    expect(screen.getByRole("link", { name: "Orkestra" })).toBeInTheDocument();

    release();
    expect(
      await screen.findByRole("button", { name: "Sign out" }),
    ).toBeInTheDocument();
    expect(screen.queryByRole("link", { name: "Sign in" })).toBeNull();
    // /policy gates anonymous-only UI. It fired neither DURING the bootstrap —
    // the waste this change removes — nor after it, the user being signed in.
    expect(policyHits).toBe(0);
  });
});
