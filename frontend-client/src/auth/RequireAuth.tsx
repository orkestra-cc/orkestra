import type { ReactNode } from "react";
import { Navigate, useLocation } from "react-router";
import { useTranslation } from "react-i18next";
import { useAuth } from "@/auth/useAuth";

interface RequireAuthProps {
  children: ReactNode;
}

// Route guard: once the session is DECIDED and there is none, redirect to
// /login with the originally-requested path stamped on `?next=` so
// post-login the user lands where they were headed.
//
// The wait is the whole point (spec §8 #11). AuthProvider's bootstrap
// refresh is a fetch started in a passive effect, i.e. strictly after the
// first commit, so on every cold load / reload / deep link the first
// render sees `token === null` for a user whose refresh cookie is
// perfectly valid. Redirecting there replaced the history entry before the
// refresh had even left, and nothing navigated back: a signed-in user got
// a login form under a signed-in header. So while the bootstrap is in
// flight we render a status line instead of the route. The window is
// ONCE PER DOCUMENT, not once per navigation: `isBootstrapping` starts
// true and AuthProvider's `finally` flips it to false for every outcome,
// including the marker-less short-circuit that never makes a request, and
// nothing ever sets it back (AuthProvider.tsx:36,51-53 —
// authContext.ts:12-13 states the invariant). So an in-app link to a
// second guarded route is decided synchronously and paints nothing extra;
// only a cold load, a reload or a deep link pays it — tens of
// milliseconds locally, the full round-trip on a slow /refresh-cookie,
// which is why the wait says so out loud rather than showing an empty
// <main> under a signed-out header.
export function RequireAuth({ children }: RequireAuthProps) {
  const { isAuthenticated, isBootstrapping } = useAuth();
  const location = useLocation();
  const { t } = useTranslation();

  if (isBootstrapping) {
    return (
      <p
        role="status"
        aria-busy="true"
        className="px-6 py-16 text-sm text-slate-600"
      >
        {t("loading")}
      </p>
    );
  }

  if (!isAuthenticated) {
    const next = encodeURIComponent(location.pathname + location.search);
    return <Navigate to={`/login?next=${next}`} replace />;
  }
  // Preserved for ARIA — the tree below it is the actual content.
  return (
    <div aria-label={t("account.title")} className="contents">
      {children}
    </div>
  );
}
