import { Link, NavLink, Outlet, useNavigate } from "react-router";
import { useQuery } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";

import { fetchAuthPolicy, passwordLoginUsable } from "@/api/auth";
import { LanguageSwitcher } from "@/components/LanguageSwitcher";
import { UserAvatar } from "@/components/UserAvatar";
import { useAuth } from "@/auth/useAuth";
import { useMe } from "@/auth/useMe";

export function Layout() {
  const { t } = useTranslation();
  // isBootstrapping, not isAuthenticated alone: `token !== null` is false
  // for the WHOLE cold-load window, so a returning user would otherwise see
  // the anonymous header flash and pay for an anonymous-only /policy fetch
  // (§8 #18b — the #11 defect, in the header rather than the route guard).
  const { isAuthenticated, isBootstrapping, signOut } = useAuth();
  const { data: me } = useMe();
  const navigate = useNavigate();
  // Hide the prominent "Sign up" CTA when self-service registration or
  // password sign-in is off — visiting /signup directly still swaps the
  // form for a full-page notice in the off case, but most users discover
  // the route via the header. Same cache key used by /login, /signup and
  // /forgot-password, so all four surfaces share one fetch.
  //
  // Gated on the bootstrap too: this policy drives anonymous-only UI, and
  // during the cold-load window we do not yet know the visitor is anonymous.
  const { data: policy } = useQuery({
    queryKey: ["authPolicy"],
    queryFn: fetchAuthPolicy,
    staleTime: 30_000,
    enabled: !isBootstrapping && !isAuthenticated,
  });
  const registrationEnabled = policy?.registrationEnabled ?? true;
  const passwordOn = passwordLoginUsable(policy);

  async function handleSignOut() {
    await signOut();
    navigate("/", { replace: true });
  }

  return (
    <div className="flex min-h-screen flex-col">
      <header className="border-b border-slate-200 bg-white">
        <div className="mx-auto flex max-w-6xl items-center justify-between px-6 py-4">
          <Link to="/" className="text-xl font-semibold tracking-tight">
            {t("app.name")}
          </Link>
          <nav className="flex items-center gap-2">
            {/* Nothing in the auth slot until the bootstrap settles — not a
                spinner: the window is one /refresh-cookie round-trip and a
                spinner in a header reads as breakage. The logo, the language
                switcher and the footer stay, so the layout does not shift. */}
            {isBootstrapping ? null : isAuthenticated ? (
              <>
                <NavLink
                  to="/account"
                  className={({ isActive }) =>
                    [
                      "flex items-center gap-2 rounded-md px-2 py-1 text-sm font-medium transition-colors",
                      isActive
                        ? "bg-slate-100 text-slate-900"
                        : "text-slate-600 hover:bg-slate-100 hover:text-slate-900",
                    ].join(" ")
                  }
                >
                  <UserAvatar user={me} size="xs" />
                  <span>{t("nav.account")}</span>
                </NavLink>
                <button
                  type="button"
                  onClick={handleSignOut}
                  className="rounded-md px-3 py-2 text-sm font-medium text-slate-600 hover:bg-slate-100 hover:text-slate-900"
                >
                  {t("nav.signout")}
                </button>
              </>
            ) : (
              <>
                <Link
                  to="/login"
                  className="rounded-md px-3 py-2 text-sm font-medium text-slate-600 hover:bg-slate-100 hover:text-slate-900"
                >
                  {t("nav.signin")}
                </Link>
                {registrationEnabled && passwordOn && (
                  <Link
                    to="/signup"
                    className="rounded-md bg-slate-900 px-3 py-2 text-sm font-medium text-white hover:bg-slate-700"
                  >
                    {t("nav.signup")}
                  </Link>
                )}
              </>
            )}
            <LanguageSwitcher />
          </nav>
        </div>
      </header>
      <main className="flex-1">
        <Outlet />
      </main>
      <footer className="border-t border-slate-200 bg-white py-6 text-center text-xs text-slate-500">
        © {new Date().getFullYear()} {t("app.name")}
      </footer>
    </div>
  );
}
