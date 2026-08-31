import { describe, expect, it } from "vitest";

import {
  OAUTH_PROVIDERS,
  OAUTH_PROVIDER_LABELS,
  isOAuthProvider,
} from "@/lib/oauthProviders";

describe("oauthProviders", () => {
  it("is exactly the four backend providers, each with a label", () => {
    expect([...OAUTH_PROVIDERS]).toEqual([
      "google",
      "apple",
      "github",
      "discord",
    ]);
    for (const p of OAUTH_PROVIDERS)
      expect(OAUTH_PROVIDER_LABELS[p]).toBeTruthy();
  });

  it.each([["google"], ["apple"], ["github"], ["discord"]])(
    "accepts %s",
    (name) => expect(isOAuthProvider(name)).toBe(true),
  );

  it.each([["facebook"], ["Google"], [""], [null], [undefined], [42], [{}]])(
    "rejects %j",
    (value) => expect(isOAuthProvider(value)).toBe(false),
  );
});
