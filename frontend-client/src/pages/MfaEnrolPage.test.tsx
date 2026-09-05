import { describe, expect, it } from "vitest";
import { http, HttpResponse } from "msw";
import { screen } from "@testing-library/react";

import en from "@/locales/en.json";
import { MfaEnrolPage } from "@/pages/MfaEnrolPage";
import { url } from "@/test/handlers";
import { renderWithProviders } from "@/test/render";
import { server } from "@/test/server";

const MFA_STATUS = url("/v1/auth/client/me/mfa");

// No token and no session marker is deliberate: AuthProvider's mount refresh
// short-circuits without the marker, so this file needs no /refresh-cookie
// stub, and the page's own read is answered by MSW either way. Adding a token
// would arm the bootstrap refresh and fail the suite on an unhandled request
// (src/test/setup.ts runs MSW with onUnhandledRequest: "error").
const statusHandler = (body: Record<string, unknown>) =>
  server.use(
    http.get(MFA_STATUS, () =>
      HttpResponse.json({
        backupCodesRemaining: 0,
        requiresMfa: false,
        webauthnCredentials: 0,
        ...body,
      }),
    ),
  );

describe("MfaEnrolPage reads /me/mfa before offering a wizard (§4.2 D14)", () => {
  // An enrolled client user reaching this page is REPLACING a factor, and the
  // backend's RequireEnrolmentProof gate answers a replacement with
  // step_up_required — a code this SPA has no modal for, and deliberately is
  // not growing one. Without the read they get a wizard whose first request is
  // a 401 they cannot act on. The supported route is an operator admin reset.
  it("renders the enrolled state instead of the wizard when the factor exists", async () => {
    statusHandler({
      status: "enrolled",
      type: "totp",
      backupCodesRemaining: 8,
    });

    renderWithProviders(<MfaEnrolPage />);

    expect(
      await screen.findByText(en.mfa.enrol.alreadyTitle),
    ).toBeInTheDocument();
    expect(screen.getByText(en.mfa.enrol.alreadyBody)).toBeInTheDocument();
    expect(
      screen.queryByRole("button", { name: en.mfa.enrol.start }),
    ).toBeNull();
    // A key missing from the bundle renders as itself.
    expect(document.body.textContent).not.toContain("mfa.enrol.");
  });

  it("renders the wizard when no factor is enrolled", async () => {
    statusHandler({ status: "not_required" });

    renderWithProviders(<MfaEnrolPage />);

    expect(
      await screen.findByRole("button", { name: en.mfa.enrol.start }),
    ).toBeEnabled();
    expect(screen.queryByText(en.mfa.enrol.alreadyTitle)).toBeNull();
  });

  // The read is UX, not enforcement: the gate is the backend's, and it refuses
  // a replacement whatever this page renders. So only the definite answer
  // "enrolled" hides the wizard — a status blip must never be the reason a
  // first enrolment cannot be started.
  it("fails open to the wizard when the status read errors", async () => {
    server.use(
      http.get(MFA_STATUS, () => HttpResponse.json({}, { status: 500 })),
    );

    renderWithProviders(<MfaEnrolPage />);

    expect(
      await screen.findByRole("button", { name: en.mfa.enrol.start }),
    ).toBeEnabled();
    expect(screen.queryByText(en.mfa.enrol.alreadyTitle)).toBeNull();
  });
});
