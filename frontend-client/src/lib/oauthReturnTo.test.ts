import { describe, expect, it } from "vitest";

import {
  OAUTH_RETURN_TO_KEY,
  OAUTH_RETURN_TO_TTL_MS,
  stashOAuthReturnTo,
  takeOAuthReturnTo,
} from "@/lib/oauthReturnTo";

const stored = () => sessionStorage.getItem(OAUTH_RETURN_TO_KEY);

describe("OAuth return-target record", () => {
  it("stashes a sanitized target with its creation time", () => {
    stashOAuthReturnTo("/account/security?tab=oauth", 1_000);
    expect(JSON.parse(stored()!)).toEqual({
      target: "/account/security?tab=oauth",
      createdAt: 1_000,
    });
  });

  it("removes any previous record when the new target is unsafe or absent", () => {
    stashOAuthReturnTo("/account", 1_000);
    stashOAuthReturnTo("//evil.example", 2_000);
    expect(stored()).toBeNull();
    stashOAuthReturnTo("/account", 3_000);
    stashOAuthReturnTo(null, 4_000);
    expect(stored()).toBeNull();
  });

  it("take-and-deletes on every call, even when nothing is stored", () => {
    expect(takeOAuthReturnTo()).toBeNull();
    stashOAuthReturnTo("/account", 1_000);
    expect(takeOAuthReturnTo(1_500)).toBe("/account");
    expect(stored()).toBeNull();
    expect(takeOAuthReturnTo(1_600)).toBeNull();
  });

  it("honours a record up to ten minutes old and ignores an older one", () => {
    stashOAuthReturnTo("/account", 1_000);
    expect(takeOAuthReturnTo(1_000 + OAUTH_RETURN_TO_TTL_MS)).toBe("/account");
    stashOAuthReturnTo("/account", 1_000);
    expect(takeOAuthReturnTo(1_000 + OAUTH_RETURN_TO_TTL_MS + 1)).toBeNull();
    expect(stored()).toBeNull();
  });

  it("ignores a record from the future", () => {
    stashOAuthReturnTo("/account", 5_000);
    expect(takeOAuthReturnTo(4_999)).toBeNull();
  });

  it.each([
    "not json",
    "null",
    "42",
    JSON.stringify({ target: "/account" }),
    JSON.stringify({ target: "/account", createdAt: "1000" }),
    JSON.stringify({ target: "/account", createdAt: Number.NaN }),
    JSON.stringify({ createdAt: 1_000 }),
    JSON.stringify({ target: "//evil.example", createdAt: 1_000 }),
    JSON.stringify({ target: "/auth/callback", createdAt: 1_000 }),
    JSON.stringify({ target: "/LOGIN", createdAt: 1_000 }),
  ])("re-validates a client-written record and rejects %s", (raw) => {
    sessionStorage.setItem(OAUTH_RETURN_TO_KEY, raw);
    expect(takeOAuthReturnTo(1_500)).toBeNull();
    expect(stored()).toBeNull();
  });

  it("uses Date.now() when no clock is supplied", () => {
    stashOAuthReturnTo("/account");
    expect(takeOAuthReturnTo()).toBe("/account");
  });
});
