import { Link } from 'react-router-dom';
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
  const isAuthenticated = useAppSelector(selectIsAuthenticated);
  const user = useAppSelector(selectUser);
  const { data: mfa } = useGetMfaStatusQuery(undefined, {
    skip: !isAuthenticated || !user,
  });

  if (!isAuthenticated || !user) return null;
  if (!mfa || !mfa.requiresMfa || mfa.status === 'enrolled') return null;

  const remaining = formatGraceRemaining(mfa.graceExpiresAt);
  const expired = remaining === 'expired';

  return (
    <div
      role="alert"
      className="d-flex align-items-center justify-content-between px-3 py-2 border-bottom"
      style={{
        background: expired ? '#f8d7da' : '#fff3cd',
        color: expired ? '#58151c' : '#664d03',
        fontSize: '0.875rem',
      }}
    >
      <span>
        <strong>🔐 Two-factor authentication required</strong>
        <span className="ms-2">
          {expired
            ? 'Your grace window has expired. You will be locked out on next sign-in until you enroll.'
            : remaining
              ? `Your role requires MFA. ${remaining} to enroll before sign-in is blocked.`
              : 'Your role requires MFA. Set it up to avoid being locked out of sign-in.'}
        </span>
      </span>
      <Link to="/user/settings" className="btn btn-sm btn-dark">
        Set up
      </Link>
    </div>
  );
}

// Formats the delta between now and graceExpiresAt as a short human string.
// Returns "" when no deadline is set (grace clock not started), and the
// literal "expired" when the deadline has passed — the caller uses that
// sentinel to switch the banner to the red variant.
function formatGraceRemaining(iso: string | null | undefined): string {
  if (!iso) return '';
  const deadlineMs = new Date(iso).getTime();
  if (!Number.isFinite(deadlineMs)) return '';
  const diff = deadlineMs - Date.now();
  if (diff <= 0) return 'expired';
  const days = Math.floor(diff / (24 * 60 * 60 * 1000));
  if (days >= 2) return `${days} days left`;
  const hours = Math.floor(diff / (60 * 60 * 1000));
  if (hours >= 2) return `${hours} hours left`;
  const minutes = Math.max(1, Math.floor(diff / (60 * 1000)));
  return `${minutes} minutes left`;
}
