import { describe, expect, it } from "vitest";
import { http, HttpResponse } from "msw";
import { screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";

import { ForgotPasswordPage } from "@/pages/ForgotPasswordPage";
import { clientPolicyHandler, url } from "@/test/handlers";
import { renderWithProviders, waitForQuerySettled } from "@/test/render";
import { server } from "@/test/server";

const FORGOT = url("/v1/auth/client/forgot-password");
const POLICY = url("/v1/auth/client/policy");

const submit = async (email = "a@b.c") => {
  const user = userEvent.setup();
  await user.type(await screen.findByLabelText(/^email$/i), email);
  await user.click(screen.getByRole("button", { name: /send link/i }));
};

describe("ForgotPasswordPage — password-off notice (D8, spec §4.3 gate)", () => {
  it("replaces the form with the notice and a back link when the method is off", async () => {
    server.use(clientPolicyHandler({ passwordLoginEnabled: false }));
    renderWithProviders(<ForgotPasswordPage />);
    expect(await screen.findByRole("alert")).toHaveTextContent(
      /password sign-in is disabled here/i,
    );
    expect(screen.queryByLabelText(/^email$/i)).toBeNull();
    expect(
      screen.getByRole("link", { name: /back to sign in/i }),
    ).toHaveAttribute("href", "/login");
  });

  it("keeps the form when the method is on", async () => {
    server.use(clientPolicyHandler());
    const { queryClient } = renderWithProviders(<ForgotPasswordPage />);
    await waitForQuerySettled(queryClient, ["authPolicy"]);
    expect(screen.getByLabelText(/^email$/i)).toBeInTheDocument();
    expect(screen.queryByRole("alert")).toBeNull();
  });
});

describe("ForgotPasswordPage — the form shows the backend's answer (spec §4.10 'direct forms')", () => {
  it("503 auth.policy_unavailable → the shared retryable copy, form kept, button re-enabled", async () => {
    server.use(
      clientPolicyHandler(),
      http.post(FORGOT, () =>
        HttpResponse.json(
          { code: "auth.policy_unavailable", detail: "raw-detail" },
          { status: 503 },
        ),
      ),
    );
    renderWithProviders(<ForgotPasswordPage />);
    await submit();
    expect(await screen.findByRole("alert")).toHaveTextContent(
      /sign-in policy is temporarily unavailable/i,
    );
    expect(screen.queryByText(/raw-detail/)).toBeNull();
    expect(screen.getByLabelText(/^email$/i)).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /send link/i })).toBeEnabled();
  });

  it("403 auth.password_login_disabled while the display fell open → the disabled notice", async () => {
    // /policy is down (fail-open display) but the backend still gates.
    server.use(
      http.get(POLICY, () =>
        HttpResponse.json({ code: "auth.policy_unavailable" }, { status: 503 }),
      ),
      http.post(FORGOT, () =>
        HttpResponse.json(
          { code: "auth.password_login_disabled", detail: "raw-detail" },
          { status: 403 },
        ),
      ),
    );
    renderWithProviders(<ForgotPasswordPage />);
    await submit();
    expect(await screen.findByRole("alert")).toHaveTextContent(
      /password sign-in is disabled here/i,
    );
    expect(screen.queryByText(/raw-detail/)).toBeNull();
  });

  it("any other failure → the generic error, never the raw detail", async () => {
    server.use(
      clientPolicyHandler(),
      http.post(FORGOT, () =>
        HttpResponse.json({ detail: "raw-detail" }, { status: 500 }),
      ),
    );
    renderWithProviders(<ForgotPasswordPage />);
    await submit();
    expect(await screen.findByRole("alert")).toHaveTextContent(
      /something went wrong/i,
    );
    expect(screen.queryByText(/raw-detail/)).toBeNull();
  });

  // Regression guard — GREEN before the change: the neutral confirmation.
  it("200 → the neutral confirmation", async () => {
    server.use(
      clientPolicyHandler(),
      http.post(FORGOT, () => HttpResponse.json({ success: true })),
    );
    renderWithProviders(<ForgotPasswordPage />);
    await submit();
    expect(await screen.findByText(/check your email/i)).toBeInTheDocument();
  });
});
