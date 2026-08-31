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
  it("renders through QueryClient + AuthProvider + MemoryRouter and guards a route", () => {
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
    expect(screen.getByTestId("login-location")).toHaveTextContent(
      "/login?next=%2Faccount%2Fsecurity",
    );
    expect(screen.queryByTestId("secret-location")).toBeNull();
  });

  it("resolves copy in English (en.json nav.signin)", () => {
    renderWithProviders(<EnglishCopy />);
    expect(screen.getByText("Sign in")).toBeInTheDocument();
  });
});
