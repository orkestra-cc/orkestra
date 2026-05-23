import { Link } from 'react-router-dom';
import { useTranslation } from 'react-i18next';
import { useAppSelector } from 'store/hooks';
import { selectIsAuthenticated, selectUser } from 'store/slices/authSlice';
import { useGetMfaStatusQuery } from 'store/api/mfaApi';

/**
 * Nags privileged users who haven't set up a second factor. Server returns
 * `requiresMfa: true` when the caller's system role or any org-scoped role
 * obligates enrollment, plus a `graceExpiresAt` deadline (if the clock has
 * started) so we can render a countdown. Silent for users who don't need
 * MFA, users already enrolled, and anonymous visitors.
 */
export default function MfaEnrollmentBanner() {
  const { t } = useTranslation();
  const isAuthenticated = useAppSelector(selectIsAuthenticated);
  const user = useAppSelector(selectUser);
  const { data: mfa } = useGetMfaStatusQuery(undefined, {
    skip: !isAuthenticated || !user
  });

  if (!isAuthenticated || !user) return null;
  if (!mfa || !mfa.requiresMfa || mfa.status === 'enrolled') return null;

  const grace = gradeRemaining(mfa.graceExpiresAt);
  const expired = grace.kind === 'expired';

  const remainingLabel =
    grace.kind === 'days'
      ? t('banner.mfa.daysLeft', { count: grace.n })
      : grace.kind === 'hours'
        ? t('banner.mfa.hoursLeft', { count: grace.n })
        : grace.kind === 'minutes'
          ? t('banner.mfa.minutesLeft', { count: grace.n })
          : '';

  return (
    <div
      role="alert"
      className="d-flex align-items-center justify-content-between px-3 py-2 border-bottom"
      style={{
        background: expired ? '#f8d7da' : '#fff3cd',
        color: expired ? '#58151c' : '#664d03',
        fontSize: '0.875rem'
      }}
    >
      <span>
        <strong>🔐 {t('banner.mfa.title')}</strong>
        <span className="ms-2">
          {expired
            ? t('banner.mfa.expired')
            : remainingLabel
              ? t('banner.mfa.remaining', { remaining: remainingLabel })
              : t('banner.mfa.required')}
        </span>
      </span>
      <Link to="/user/settings" className="btn btn-sm btn-dark">
        {t('banner.mfa.setUp')}
      </Link>
    </div>
  );
}

type GraceState =
  | { kind: 'none' }
  | { kind: 'expired' }
  | { kind: 'days'; n: number }
  | { kind: 'hours'; n: number }
  | { kind: 'minutes'; n: number };

// Pure helper — returns a discriminated structure so the renderer picks
// the matching i18next key + pluralization without the helper needing
// to know about t().
function gradeRemaining(iso: string | null | undefined): GraceState {
  if (!iso) return { kind: 'none' };
  const deadlineMs = new Date(iso).getTime();
  if (!Number.isFinite(deadlineMs)) return { kind: 'none' };
  const diff = deadlineMs - Date.now();
  if (diff <= 0) return { kind: 'expired' };
  const days = Math.floor(diff / (24 * 60 * 60 * 1000));
  if (days >= 2) return { kind: 'days', n: days };
  const hours = Math.floor(diff / (60 * 60 * 1000));
  if (hours >= 2) return { kind: 'hours', n: hours };
  const minutes = Math.max(1, Math.floor(diff / (60 * 1000)));
  return { kind: 'minutes', n: minutes };
}
