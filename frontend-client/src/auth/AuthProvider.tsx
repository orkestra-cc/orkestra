import {
  useCallback,
  useEffect,
  useMemo,
  useState,
  type ReactNode,
} from "react";

import {
  bootstrapFromRefreshCookie as bootstrapFromRefreshCookieStore,
  clearSessionLocally,
  getAccessToken,
  refreshAccessToken,
  setAccessToken,
  subscribe,
} from "@/auth/tokenStore";
import { apiBaseURL } from "@/api/client";
import { AuthContext, type AuthState } from "@/auth/authContext";
import { setSessionMarker } from "@/auth/sessionMarker";

interface AuthProviderProps {
  children: ReactNode;
}

// AuthProvider mirrors the in-memory token store into React state so any
// component can re-render when the user logs in or out. On mount it does
// one optimistic refresh attempt — if the refresh cookie is still valid
// (returning user, page reload), the SPA boots authenticated. Phase 3
// will wire the actual login form against /v1/auth/client/login; this
// provider only owns the lifecycle, not the UI.
export function AuthProvider({ children }: AuthProviderProps) {
  const [token, setToken] = useState<string | null>(getAccessToken());
  // The cold-load window, exposed so a consumer can tell "not decided yet"
  // from "signed out" (spec §8 #11): on a fresh document the token store is
  // empty and stays empty until the mount refresh below answers.
  const [isBootstrapping, setIsBootstrapping] = useState(true);

  useEffect(() => subscribe(setToken), []);

  useEffect(() => {
    // One-shot bootstrap refresh. tokenStore.refreshAccessToken is a
    // no-op for anonymous visitors (no localStorage marker) so the
    // catalog/signup pages don't fire a guaranteed-401 on every cold
    // load. Returning users — who have stamped the marker on a prior
    // signIn — get auto-rehydrated here.
    //
    // `finally`, not `then`: every outcome ends the window — ok,
    // signed-out, unavailable, and the marker-less short-circuit that
    // never leaves. refreshAccessToken never rejects, so this is the one
    // and only flip, and a `catch` here would be dead code.
    void refreshAccessToken(apiBaseURL).finally(() =>
      setIsBootstrapping(false),
    );
  }, []);

  const signIn = useCallback((next: string, expiresInSeconds?: number) => {
    setSessionMarker();
    setAccessToken(next, expiresInSeconds);
  }, []);

  const signOut = useCallback(async () => {
    try {
      await fetch(`${apiBaseURL}/v1/auth/client/logout`, {
        method: "POST",
        credentials: "include",
      });
    } finally {
      // The one sanctioned local clear (tokenStore.ts): marker AND token, in
      // one place. Clearing them inline here is exactly how the deleted
      // client.ts middleware drifted into clearing only the token and leaving
      // a marker that short-circuited the next cold load.
      clearSessionLocally();
    }
  }, []);

  const bootstrapFromRefreshCookie = useCallback(
    () => bootstrapFromRefreshCookieStore(apiBaseURL),
    [],
  );

  const value = useMemo<AuthState>(
    () => ({
      accessToken: token,
      isAuthenticated: token !== null,
      isBootstrapping,
      signIn,
      signOut,
      bootstrapFromRefreshCookie,
    }),
    [token, isBootstrapping, signIn, signOut, bootstrapFromRefreshCookie],
  );

  return <AuthContext.Provider value={value}>{children}</AuthContext.Provider>;
}
