import { Link, NavLink, Outlet, useNavigate } from 'react-router-dom';
import { useTranslation } from 'react-i18next';

import { LanguageSwitcher } from '@/components/LanguageSwitcher';
import { useAuth } from '@/auth/useAuth';

const navLinkClass = ({ isActive }: { isActive: boolean }) =>
  [
    'rounded-md px-3 py-2 text-sm font-medium transition-colors',
    isActive
      ? 'bg-slate-100 text-slate-900'
      : 'text-slate-600 hover:bg-slate-100 hover:text-slate-900',
  ].join(' ');

export function Layout() {
  const { t } = useTranslation();
  const { isAuthenticated, signOut } = useAuth();
  const navigate = useNavigate();

  async function handleSignOut() {
    await signOut();
    navigate('/', { replace: true });
  }

  return (
    <div className="flex min-h-screen flex-col">
      <header className="border-b border-slate-200 bg-white">
        <div className="mx-auto flex max-w-6xl items-center justify-between px-6 py-4">
          <Link to="/" className="text-xl font-semibold tracking-tight">
            {t('app.name')}
          </Link>
          <nav className="flex items-center gap-2">
            <NavLink to="/catalog" className={navLinkClass}>
              {t('nav.catalog')}
            </NavLink>
            {isAuthenticated ? (
              <>
                <NavLink to="/account" className={navLinkClass}>
                  {t('nav.account')}
                </NavLink>
                <button
                  type="button"
                  onClick={handleSignOut}
                  className="rounded-md px-3 py-2 text-sm font-medium text-slate-600 hover:bg-slate-100 hover:text-slate-900"
                >
                  {t('nav.signout')}
                </button>
              </>
            ) : (
              <>
                <Link
                  to="/login"
                  className="rounded-md px-3 py-2 text-sm font-medium text-slate-600 hover:bg-slate-100 hover:text-slate-900"
                >
                  {t('nav.signin')}
                </Link>
                <Link
                  to="/signup"
                  className="rounded-md bg-slate-900 px-3 py-2 text-sm font-medium text-white hover:bg-slate-700"
                >
                  {t('nav.signup')}
                </Link>
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
        © {new Date().getFullYear()} {t('app.name')}
      </footer>
    </div>
  );
}
