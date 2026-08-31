import { describe, expect, it } from "vitest";
import { Route, Routes } from "react-router";
import { screen, waitFor } from "@testing-library/react";

import { Layout } from "@/components/Layout";
import { clientPolicyHandler } from "@/test/handlers";
import { renderWithProviders, waitForQuerySettled } from "@/test/render";
import { server } from "@/test/server";

const renderLayout = () =>
  renderWithProviders(
    <Routes>
      <Route element={<Layout />}>
        <Route path="/" element={<div>home</div>} />
      </Route>
    </Routes>,
  );

describe("Layout — anonymous header CTA (spec §4.10)", () => {
  it("hides the Sign-up CTA once the policy says password sign-in is off", async () => {
    server.use(clientPolicyHandler({ passwordLoginEnabled: false }));
    renderLayout();
    // The first paint falls open (CTA present); its disappearance is the
    // settled anchor.
    expect(screen.getByRole("link", { name: "Sign up" })).toBeInTheDocument();
    await waitFor(() =>
      expect(
        screen.queryByRole("link", { name: "Sign up" }),
      ).not.toBeInTheDocument(),
    );
    expect(screen.getByRole("link", { name: "Sign in" })).toBeInTheDocument();
  });

  it("keeps the CTA when the method is on", async () => {
    server.use(clientPolicyHandler());
    const { queryClient } = renderLayout();
    await waitForQuerySettled(queryClient, ["authPolicy"]);
    expect(screen.getByRole("link", { name: "Sign up" })).toBeInTheDocument();
  });

  // Regression guard — GREEN before the change.
  it("still hides the CTA when self-service registration is off", async () => {
    server.use(clientPolicyHandler({ registrationEnabled: false }));
    renderLayout();
    expect(screen.getByRole("link", { name: "Sign up" })).toBeInTheDocument();
    await waitFor(() =>
      expect(
        screen.queryByRole("link", { name: "Sign up" }),
      ).not.toBeInTheDocument(),
    );
  });
});
