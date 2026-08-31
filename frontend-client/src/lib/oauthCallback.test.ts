import { describe, expect, it } from "vitest";

import {
  OAUTH_CALLBACK_ERROR_KEYS,
  parseOAuthCallback,
} from "@/lib/oauthCallback";

const generic = { kind: "error", errorKey: "loginFailed" } as const;

describe("parseOAuthCallback — closed contract", () => {
  it.each(["google", "apple", "github", "discord"] as const)(
    "success: ?success=true&provider=%s",
    (provider) => {
      expect(
        parseOAuthCallback(`?success=true&provider=${provider}`, ""),
      ).toEqual({ kind: "success", provider });
    },
  );

  it("failure: every allowlisted code maps to its i18n key", () => {
    for (const [code, key] of Object.entries(OAUTH_CALLBACK_ERROR_KEYS)) {
      expect(
        parseOAuthCallback(
          `?success=false&error=${encodeURIComponent(code)}`,
          "",
        ),
      ).toEqual({ kind: "error", errorKey: key });
    }
    expect(Object.keys(OAUTH_CALLBACK_ERROR_KEYS).sort()).toEqual(
      [
        "auth.oauth_email_unverified",
        "oauth_access_denied",
        "oauth_link_disabled",
        "oauth_login_failed",
        "oauth_provider_unavailable",
        "oauth_signup_disabled",
      ].sort(),
    );
  });

  it("failure: an unknown, empty or hostile code collapses to loginFailed", () => {
    expect(
      parseOAuthCallback("?success=false&error=internal_stack_trace", ""),
    ).toEqual(generic);
    expect(parseOAuthCallback("?success=false&error=", "")).toEqual(generic);
    expect(
      parseOAuthCallback(
        "?success=false&error=%3Cscript%3Ealert(1)%3C%2Fscript%3E",
        "",
      ),
    ).toEqual(generic);
  });

  it("MFA: exactly the three fragment keys with an empty query", () => {
    expect(
      parseOAuthCallback(
        "",
        "#requiresMfa=true&mfaToken=ch-1&webauthnAvailable=false",
      ),
    ).toEqual({ kind: "mfa", challengeId: "ch-1", webauthnAvailable: false });
    expect(
      parseOAuthCallback(
        "",
        "requiresMfa=true&mfaToken=ch-1&webauthnAvailable=true",
      ),
    ).toEqual({ kind: "mfa", challengeId: "ch-1", webauthnAvailable: true });
  });

  it.each([
    ["success with an unknown provider", "?success=true&provider=facebook", ""],
    [
      "success with an extra key",
      "?success=true&provider=google&email=a%40b.c",
      "",
    ],
    [
      "success with a duplicated key",
      "?success=true&provider=google&provider=github",
      "",
    ],
    [
      "success next to an MFA fragment",
      "?success=true&provider=google",
      "#requiresMfa=true&mfaToken=ch-1&webauthnAvailable=false",
    ],
    ["success next to any fragment", "?success=true&provider=google", "#x=1"],
    ["failure missing error", "?success=false", ""],
    [
      "failure with an extra key",
      "?success=false&error=oauth_access_denied&user_id=u1",
      "",
    ],
    ["success=maybe", "?success=maybe&provider=google", ""],
    ["MFA missing the token", "", "#requiresMfa=true&webauthnAvailable=false"],
    [
      "MFA with an empty token",
      "",
      "#requiresMfa=true&mfaToken=&webauthnAvailable=false",
    ],
    [
      "MFA with requiresMfa=false",
      "",
      "#requiresMfa=false&mfaToken=ch-1&webauthnAvailable=false",
    ],
    [
      "MFA with a non-boolean webauthnAvailable",
      "",
      "#requiresMfa=true&mfaToken=ch-1&webauthnAvailable=yes",
    ],
    [
      "MFA with an extra key",
      "",
      "#requiresMfa=true&mfaToken=ch-1&webauthnAvailable=false&access_token=x",
    ],
    [
      "MFA fragment next to any query",
      "?x=1",
      "#requiresMfa=true&mfaToken=ch-1&webauthnAvailable=false",
    ],
    ["nothing at all", "", ""],
    ["unrelated query", "?foo=bar", ""],
  ])("%s is the generic failure", (_label, search, hash) => {
    expect(parseOAuthCallback(search, hash)).toEqual(generic);
  });
});
