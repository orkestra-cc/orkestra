import { Link, NavLink, Outlet, useNavigate } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";

import { fetchAuthPolicy } from "@/api/auth";
import { LanguageSwitcher } from "@/components/LanguageSwitcher";
import { UserAvatar } from "@/components/UserAvatar";
import { useAuth } from "@/auth/useAuth";
import { useMe } from "@/auth/useMe";

const navLinkClass = ({ isActive }: { isActive: boolean }) =>
  [
    "rounded-md px-3 py-2 text-sm font-medium transition-colors",
    isActive
      ? "bg-slate-100 text-slate-900"
      : "text-slate-600 hover:bg-slate-100 hover:text-slate-900",
  ].join(" ");

export function Layout() {
  const { t } = useTranslation();
  const { isAuthenticated, signOut } = useAuth();
  const { data: me } = useMe();
  const navigate = useNavigate();
  // Hide the prominent "Sign up" CTA when self-service registration is
  // off — visiting /signup directly still renders a banner via the page
  // itself, but most users discover the route via the header. Same
  // cache key used by /login + /signup so all three share one fetch.
  const { data: policy } = useQuery({
    queryKey: ["authPolicy"],
    queryFn: fetchAuthPolicy,
    staleTime: 30_000,
    enabled: !isAuthenticated,
  });
  const registrationEnabled = policy?.registrationEnabled ?? true;

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
            <NavLink to="/catalog" className={navLinkClass}>
              {t("nav.catalog")}
            </NavLink>
            {isAuthenticated ? (
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
                {registrationEnabled && (
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
