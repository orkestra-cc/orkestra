import { afterEach, describe, expect, it } from "vitest";
import { screen } from "@testing-library/react";

import i18n from "@/i18n";
import en from "@/locales/en.json";
import itBundle from "@/locales/it.json";
import { AccountDsrPage } from "@/pages/AccountDsrPage";
import { renderWithProviders } from "@/test/render";

// The page mounts no query and fires no request until a button is pressed,
// so no MSW stub is needed to render it (the AuthProvider's mount refresh
// short-circuits on the missing session marker).
//
// src/test/setup.ts pins the language once, in a beforeAll — so a case that
// switches it owns putting it back, or every file that runs after this one
// asserts English copy against an Italian bundle.
afterEach(async () => {
  await i18n.changeLanguage("en");
});

describe("AccountDsrPage — copy comes from the bundles, not the JSX", () => {
  it("renders the English bundle and leaks no lookup key", () => {
    renderWithProviders(<AccountDsrPage />);

    expect(
      screen.getByRole("heading", { level: 1, name: en.dsr.title }),
    ).toBeInTheDocument();
    expect(screen.getByText(en.dsr.subtitle)).toBeInTheDocument();
    expect(
      screen.getByRole("heading", { level: 2, name: en.dsr.export.title }),
    ).toBeInTheDocument();
    expect(
      screen.getByRole("button", { name: en.dsr.export.submit }),
    ).toBeEnabled();
    expect(
      screen.getByRole("heading", { level: 2, name: en.dsr.erasure.title }),
    ).toBeInTheDocument();
    expect(
      screen.getByRole("button", { name: en.dsr.erasure.submit }),
    ).toBeEnabled();
    expect(
      screen.getByPlaceholderText(en.dsr.erasure.reasonPlaceholder),
    ).toBeInTheDocument();

    // A key that exists in neither bundle renders as itself. This is the
    // assertion that would have caught the page before this change too:
    // then every string was hard-coded, so none of them was a key at all.
    expect(document.body.textContent).not.toContain("dsr.");
  });

  it("renders the Italian bundle when the locale is `it`", async () => {
    await i18n.changeLanguage("it");
    renderWithProviders(<AccountDsrPage />);

    expect(
      screen.getByRole("heading", { level: 1, name: itBundle.dsr.title }),
    ).toBeInTheDocument();
    expect(screen.getByText(itBundle.dsr.subtitle)).toBeInTheDocument();
    expect(
      screen.getByRole("button", { name: itBundle.dsr.export.submit }),
    ).toBeEnabled();
    expect(
      screen.getByRole("button", { name: itBundle.dsr.erasure.submit }),
    ).toBeEnabled();
    expect(
      screen.getByPlaceholderText(itBundle.dsr.erasure.reasonPlaceholder),
    ).toBeInTheDocument();
    // The English heading is gone — the page is not falling back.
    expect(screen.queryByText(en.dsr.title)).toBeNull();
  });
});
