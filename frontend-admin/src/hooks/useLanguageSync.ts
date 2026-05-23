import { useEffect } from 'react';
import { useTranslation } from 'react-i18next';
import { useAppSelector } from 'store/hooks';
import { selectUser } from 'store/slices/authSlice';

/**
 * Reconciles i18next's active language with the authenticated user's
 * server-stored preference. Mounted once near the root of the tree
 * (currently in App.tsx) so a login response that carries a different
 * `user.language` than the cookie/navigator detection re-renders the
 * tree in the user's chosen language.
 *
 * No-op when no user is signed in or when the user document carries no
 * language — the i18n bootstrap's detector chain (cookie → navigator)
 * already picked the pre-login language. Empty `language` should not
 * happen after Phase 1's backfill, but we tolerate it because the auth
 * layer talks to other clients (CLI, future mobile) that may legitimately
 * omit the field.
 */
export function useLanguageSync(): void {
  const user = useAppSelector(selectUser);
  const { i18n } = useTranslation();
  const desired = user?.language;

  useEffect(() => {
    if (!desired) return;
    if (desired === i18n.language) return;
    void i18n.changeLanguage(desired);
  }, [desired, i18n]);
}

export default useLanguageSync;
