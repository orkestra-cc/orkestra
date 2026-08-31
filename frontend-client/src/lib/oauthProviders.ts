// The closed set of web OAuth providers the backend can return from
// GET /v1/auth/client/providers and accept on POST /v1/auth/client/oauth/login
// (backend/internal/core/auth/models OAuthProvider). A name outside this
// union is never rendered and never sent: fetchOAuthProviders drops it
// with a console warning, and the callback parser treats it as the
// generic failure. Adding a fifth provider is a deliberate change here,
// in the labels below and in both locale bundles.
export const OAUTH_PROVIDERS = [
  "google",
  "apple",
  "github",
  "discord",
] as const;
export type OAuthProviderName = (typeof OAUTH_PROVIDERS)[number];

export function isOAuthProvider(value: unknown): value is OAuthProviderName {
  return (
    typeof value === "string" &&
    (OAUTH_PROVIDERS as readonly string[]).includes(value)
  );
}

// Display names are brand names, not translated copy.
export const OAUTH_PROVIDER_LABELS: Record<OAuthProviderName, string> = {
  google: "Google",
  apple: "Apple",
  github: "GitHub",
  discord: "Discord",
};
