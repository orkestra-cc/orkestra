import { createContext } from "react";

import type { RefreshOutcome } from "@/auth/tokenStore";

export interface AuthState {
  accessToken: string | null;
  isAuthenticated: boolean;
  signIn: (token: string) => void;
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
