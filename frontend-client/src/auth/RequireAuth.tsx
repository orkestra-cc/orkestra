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
// flight we render NOTHING — a few tens of milliseconds of empty <main>
// inside the shell, deliberately not a spinner: this SPA has no shared
// loading component, and inventing one here would flash on every
// navigation to a guarded route.
export function RequireAuth({ children }: RequireAuthProps) {
  const { isAuthenticated, isBootstrapping } = useAuth();
  const location = useLocation();
  const { t } = useTranslation();

  if (isBootstrapping) return null;

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
