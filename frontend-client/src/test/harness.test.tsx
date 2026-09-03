import { describe, expect, it } from "vitest";
import { Route, Routes, useLocation } from "react-router";
import { screen } from "@testing-library/react";
import { useTranslation } from "react-i18next";

import { RequireAuth } from "@/auth/RequireAuth";
import { renderWithProviders } from "@/test/render";

const Probe = ({ label }: { label: string }) => {
  const location = useLocation();
  return (
    <div data-testid={`${label}-location`}>
      {location.pathname + location.search}
    </div>
  );
};

const EnglishCopy = () => {
  const { t } = useTranslation();
  return <span>{t("nav.signin")}</span>;
};

describe("test harness", () => {
  it("renders through QueryClient + AuthProvider + MemoryRouter and guards a route", async () => {
    renderWithProviders(
      <Routes>
        <Route
          path="/account/security"
          element={
            <RequireAuth>
              <Probe label="secret" />
            </RequireAuth>
          }
        />
        <Route path="/login" element={<Probe label="login" />} />
      </Routes>,
      { routerEntries: ["/account/security"] },
    );
    // Asynchronous on purpose: the guard waits for AuthProvider's bootstrap
    // to settle before it judges the session (§8 #11). For this marker-less
    // visitor that is a single microtask — no request leaves — but it is
    // still not the first commit, so the anchor cannot be a getBy.
    expect(await screen.findByTestId("login-location")).toHaveTextContent(
      "/login?next=%2Faccount%2Fsecurity",
    );
    expect(screen.queryByTestId("secret-location")).toBeNull();
  });

  it("resolves copy in English (en.json nav.signin)", () => {
    renderWithProviders(<EnglishCopy />);
    expect(screen.getByText("Sign in")).toBeInTheDocument();
  });
});
