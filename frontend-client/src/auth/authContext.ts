import { createContext } from 'react';

export interface AuthState {
  accessToken: string | null;
  isAuthenticated: boolean;
  signIn: (token: string) => void;
  signOut: () => Promise<void>;
}

// Module-scoped React context. Kept in its own file (separate from
// AuthProvider) so eslint-plugin-react-refresh stays happy — Fast Refresh
// requires a module to export only components OR only non-components, not
// both.
export const AuthContext = createContext<AuthState | null>(null);
