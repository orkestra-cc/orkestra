import { describe, expect, it } from "vitest";
import { screen } from "@testing-library/react";

import { SignupPage } from "@/pages/SignupPage";
import { clientPolicyHandler } from "@/test/handlers";
import { renderWithProviders, waitForQuerySettled } from "@/test/render";
import { server } from "@/test/server";

describe("SignupPage — password-off (spec §4.10)", () => {
  it.each([false, null])(
    "passwordLoginEnabled=%s replaces the form with the notice and a sign-in link",
    async (value) => {
      server.use(clientPolicyHandler({ passwordLoginEnabled: value }));
      renderWithProviders(<SignupPage />);
      expect(await screen.findByRole("alert")).toHaveTextContent(
        /sign-up with email and password is disabled/i,
      );
      expect(screen.queryByLabelText(/^email$/i)).toBeNull();
      expect(
        screen.queryByRole("button", { name: /create account/i }),
      ).toBeNull();
      expect(screen.getByRole("link", { name: /sign in/i })).toHaveAttribute(
        "href",
        "/login",
      );
    },
  );

  it("keeps the form when the method is on", async () => {
    server.use(clientPolicyHandler());
    const { queryClient } = renderWithProviders(<SignupPage />);
    // The form paints before and after the policy lands — anchor on the
    // cache entry.
    await waitForQuerySettled(queryClient, ["authPolicy"]);
    expect(screen.getByLabelText(/^email$/i)).toBeInTheDocument();
    expect(
      screen.queryByText(/sign-up with email and password is disabled/i),
    ).toBeNull();
  });
});
