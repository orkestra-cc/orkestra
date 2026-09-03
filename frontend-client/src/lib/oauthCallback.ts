// The SPA side of the CLOSED OAuth callback contract
// (backend: handlers/oauth_callback_redirect.go). Each of the three shapes
// is matched on its EXACT key set and cardinality; anything else — an
// unknown provider, a half-formed MFA fragment, an extra or duplicated key,
// a fragment next to a query outcome, a query next to an MFA fragment — is
// the generic failure. Raw URL text is never surfaced: only the mapped i18n
// key is. Identical to the operator console's parser (spec §4.10, v4.4).

import { isOAuthProvider, type OAuthProviderName } from "@/lib/oauthProviders";

// Login-callback failure codes (oauth_callback_redirect.go:41-46) → keys
// under oauth.callback.errors.* in both locale bundles.
export const OAUTH_CALLBACK_ERROR_KEYS = {
  oauth_access_denied: "accessDenied",
  oauth_signup_disabled: "signupDisabled",
  oauth_link_disabled: "linkDisabled",
  "auth.oauth_email_unverified": "emailUnverified",
  oauth_provider_unavailable: "providerUnavailable",
  oauth_login_failed: "loginFailed",
} as const;

export type OAuthCallbackErrorKey =
  (typeof OAUTH_CALLBACK_ERROR_KEYS)[keyof typeof OAUTH_CALLBACK_ERROR_KEYS];

export type OAuthCallbackOutcome =
  | { kind: "success"; provider: OAuthProviderName }
  | { kind: "mfa"; challengeId: string; webauthnAvailable: boolean }
  | { kind: "error"; errorKey: OAuthCallbackErrorKey };

const GENERIC: OAuthCallbackOutcome = {
  kind: "error",
  errorKey: "loginFailed",
};

const errorKeyFor = (code: string): OAuthCallbackErrorKey =>
  Object.prototype.hasOwnProperty.call(OAUTH_CALLBACK_ERROR_KEYS, code)
    ? OAUTH_CALLBACK_ERROR_KEYS[code as keyof typeof OAUTH_CALLBACK_ERROR_KEYS]
    : "loginFailed";

/**
 * exactKeys: `params` holds exactly the given keys, each exactly once.
 * This — not "the expected keys are present" — is what makes the contract
 * closed: an extra, duplicated or missing key on either side is a payload
 * the backend never produces.
 */
const exactKeys = (
  params: URLSearchParams,
  keys: readonly string[],
): boolean => {
  const present = Array.from(new Set(params.keys()));
  if (present.length !== keys.length) return false;
  return keys.every((key) => params.getAll(key).length === 1);
};

const SUCCESS_KEYS = ["success", "provider"] as const;
const FAILURE_KEYS = ["success", "error"] as const;
const MFA_KEYS = ["requiresMfa", "mfaToken", "webauthnAvailable"] as const;

/**
 * Parse the callback URL parts against the three closed shapes:
 *   success:  query = exactly {success=true, provider∈allowlist}, fragment empty
 *   failure:  query = exactly {success=false, error},              fragment empty
 *   MFA:      fragment = exactly {requiresMfa=true, mfaToken≠"", webauthnAvailable∈true|false}, query empty
 * Anything else is the generic failure.
 */
export function parseOAuthCallback(
  search: string,
  hash: string,
): OAuthCallbackOutcome {
  const query = new URLSearchParams(search);
  const frag = new URLSearchParams(hash.startsWith("#") ? hash.slice(1) : hash);
  const queryEmpty = Array.from(query.keys()).length === 0;
  const fragEmpty = Array.from(frag.keys()).length === 0;

  if (queryEmpty && exactKeys(frag, MFA_KEYS)) {
    const token = frag.get("mfaToken");
    const webauthn = frag.get("webauthnAvailable");
    if (
      frag.get("requiresMfa") !== "true" ||
      !token ||
      (webauthn !== "true" && webauthn !== "false")
    ) {
      return GENERIC;
    }
    return {
      kind: "mfa",
      challengeId: token,
      webauthnAvailable: webauthn === "true",
    };
  }

  if (
    fragEmpty &&
    exactKeys(query, SUCCESS_KEYS) &&
    query.get("success") === "true"
  ) {
    const provider = query.get("provider");
    if (!isOAuthProvider(provider)) return GENERIC;
    return { kind: "success", provider };
  }

  if (
    fragEmpty &&
    exactKeys(query, FAILURE_KEYS) &&
    query.get("success") === "false"
  ) {
    return { kind: "error", errorKey: errorKeyFor(query.get("error") ?? "") };
  }

  return GENERIC;
}
