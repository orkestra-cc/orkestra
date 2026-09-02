import { createContext } from "react";

import type { RefreshOutcome } from "@/auth/tokenStore";

export interface AuthState {
  accessToken: string | null;
  isAuthenticated: boolean;
  // True from mount until AuthProvider's one-shot bootstrap refresh has
  // SETTLED — including the marker-less short-circuit, which settles it
  // without a request. While it is true `isAuthenticated: false` means
  // "not decided yet", not "signed out", and a consumer that acts on the
  // difference (RequireAuth, LoginPage) must wait. It never goes back to
  // true: signIn and signOut do not touch it.
  isBootstrapping: boolean;
  // The lifetime travels WITH the token: the store derives the expiry from
  // the duration at receipt (§4.5), and a caller that drops it leaves the
  // store with an unknown expiry, which reads as "live", which disables the
  // 401 recovery for that session.
  signIn: (token: string, expiresInSeconds?: number) => void;
  signOut: () => Promise<void>;
  // Adopt a refresh cookie the SPA did not set itself — the client-tier
  // OAuth relay sets it on the API host and lands on /auth/callback with
  // nothing else. See tokenStore.bootstrapFromRefreshCookie for the
  // marker/outcome semantics. Never rejects.
  bootstrapFromRefreshCookie: () => Promise<RefreshOutcome>;
}

// Module-scoped React context. Kept in its own file (separate from
// AuthProvider) so eslint-plugin-react-refresh stays happy — Fast Refresh
// requires a module to export only components OR only non-components, not
// both.
export const AuthContext = createContext<AuthState | null>(null);
