import type { ReactNode } from 'react';
import { Navigate, useLocation } from 'react-router-dom';
import { useTranslation } from 'react-i18next';
import { useAuth } from '@/auth/useAuth';

interface RequireAuthProps {
  children: ReactNode;
}

// Route guard: when isAuthenticated is false, redirect to /login with the
// originally-requested path stamped on `?next=` so post-login the user
// lands where they were headed. The bootstrap refresh in AuthProvider
// fires once on cold load — for returning users with a valid refresh
// cookie this resolves before the first render path-change, so the
// happy path doesn't see the redirect.
export function RequireAuth({ children }: RequireAuthProps) {
  const { isAuthenticated } = useAuth();
  const location = useLocation();
  const { t } = useTranslation();

  if (!isAuthenticated) {
    const next = encodeURIComponent(location.pathname + location.search);
    return <Navigate to={`/login?next=${next}`} replace />;
  }
  // Preserved for ARIA — the tree below it is the actual content.
  return (
    <div aria-label={t('account.title')} className="contents">
      {children}
    </div>
  );
}
