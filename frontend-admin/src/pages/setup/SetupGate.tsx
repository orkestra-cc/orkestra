import { ReactNode, useEffect } from 'react';
import { Alert, Button } from 'react-bootstrap';
import { Navigate, useLocation } from 'react-router';
import { useTranslation } from 'react-i18next';
import OrkestraLoader from 'components/common/OrkestraLoader';
import { useGetSetupStatusQuery } from 'store/api/setupApi';
import { useAppDispatch } from 'store/hooks';
import { resetTenantState } from 'store/slices/tenantSlice';

interface SetupGateProps {
  children: ReactNode;
}

// Stable error code the backend's 503 carries when it cannot read the
// authoritative setup phase (backend/internal/shared/setup/routes.go).
// The backend fails CLOSED on this read — it never infers a phase — so the
// frontend must mirror that: a match here is routed to a neutral,
// retryable "unavailable" screen, never the wizard and never a cached
// phase. See backend/internal/shared/setup/CLAUDE.md "Fail closed on
// every authoritative read".
const SETUP_STATUS_UNAVAILABLE_CODE = 'setup.status_unavailable';
// Backend always sends Retry-After on this 503 (routes.go hardcodes "5"),
// but fall back to something sane if a future change ever omits it.
const DEFAULT_RETRY_AFTER_SECONDS = 5;

interface SetupStatusErrorInfo {
  status?: number;
  data?: { code?: string; detail?: string };
  retryAfterSeconds?: number;
}

/**
 * Top-level guard that routes a fresh installation into the onboarding
 * wizard. Placed inside App.tsx, outside the auth gate, so that an
 * uninitialized system never leaks any other route.
 *
 * Behavior:
 *  - While the query is in flight: show a splash so nothing renders stale.
 *  - On a 503 setup.status_unavailable: the backend could not determine
 *    the real phase (a database outage, not "fresh install" — see the
 *    module comment above). Show a neutral, retryable "service
 *    unavailable" screen. Never redirect to /setup and never render
 *    children here: RTK Query never writes a failed response into `data`,
 *    so the phase checks below simply never run on this branch — the
 *    failed response can't be mistaken for a phase.
 *  - On any other query error: show a blocking "cannot reach backend"
 *    screen. We do not fall through to children — because the children
 *    path hides ProtectedRoute which would then redirect to /login and
 *    obscure the real problem (backend unreachable).
 *  - phase === 'complete': render children normally (the common case
 *    after the first install).
 *  - phase !== 'complete': force-redirect anything that isn't already
 *    under /setup to /setup.
 */
const SetupGate = ({ children }: SetupGateProps) => {
  const { t } = useTranslation();
  const location = useLocation();
  const dispatch = useAppDispatch();
  const { data, isLoading, isError, error, refetch, requestId } =
    useGetSetupStatusQuery();

  // If the backend reports the install is not yet fully set up, drop any
  // tenant state left over from a previous session (e.g. a currentOrgId
  // in localStorage from a database that has since been wiped). Otherwise
  // baseApi would attach a stale X-Tenant-ID to wizard requests and the
  // backend's tenant-resolution middleware would 403 them. Keyed off the
  // authoritative `phase`, not the derived `setupCompleted` boolean.
  useEffect(() => {
    if (data && data.phase !== 'complete') {
      dispatch(resetTenantState());
    }
  }, [data, dispatch]);

  const errorInfo = error as SetupStatusErrorInfo | undefined;
  const isServiceUnavailable =
    isError &&
    errorInfo?.status === 503 &&
    errorInfo?.data?.code === SETUP_STATUS_UNAVAILABLE_CODE;
  const retryAfterSeconds =
    typeof errorInfo?.retryAfterSeconds === 'number'
      ? errorInfo.retryAfterSeconds
      : DEFAULT_RETRY_AFTER_SECONDS;

  // Auto-retry honoring Retry-After. Keyed on `requestId` (not just the
  // booleans above) so every new failed attempt — including one triggered
  // by the manual Retry button — gets its own fresh countdown rather than
  // inheriting whatever was scheduled at mount.
  useEffect(() => {
    if (!isServiceUnavailable) return undefined;
    const timer = setTimeout(() => {
      refetch();
    }, retryAfterSeconds * 1000);
    return () => clearTimeout(timer);
  }, [isServiceUnavailable, retryAfterSeconds, requestId, refetch]);

  if (isLoading) {
    return <OrkestraLoader />;
  }

  if (isServiceUnavailable) {
    return (
      <div className="container py-6" style={{ maxWidth: 640 }}>
        <Alert variant="warning">
          <Alert.Heading>{t('setup.gate.unavailableTitle')}</Alert.Heading>
          <p className="mb-2">{t('setup.gate.unavailableBody')}</p>
          <p className="fs-10 text-muted mb-3">
            {t('setup.gate.unavailableAutoRetryHint')}
          </p>
          <Button variant="outline-warning" size="sm" onClick={() => refetch()}>
            {t('setup.gate.retry')}
          </Button>
        </Alert>
      </div>
    );
  }

  if (isError || !data) {
    const detail =
      errorInfo?.data?.detail || t('setup.gate.errorDefaultDetail');
    return (
      <div className="container py-6" style={{ maxWidth: 640 }}>
        <Alert variant="danger">
          <Alert.Heading>{t('setup.gate.errorTitle')}</Alert.Heading>
          <p className="mb-2">{t('setup.gate.errorBody')}</p>
          <p className="fs-10 text-muted mb-3">{detail}</p>
          <Button variant="outline-danger" size="sm" onClick={() => refetch()}>
            {t('setup.gate.retry')}
          </Button>
        </Alert>
      </div>
    );
  }

  const isSetupPath = location.pathname.startsWith('/setup');

  if (data.phase !== 'complete' && !isSetupPath) {
    return <Navigate to="/setup" replace />;
  }

  // Note: we intentionally do NOT redirect away from /setup when phase
  // becomes 'complete'. The createInitialAdmin mutation invalidates the
  // Setup tag, which makes this query refetch and flip mid-wizard — if we
  // bounced to /dashboard here, the wizard would never get past the
  // Administrator step to the Organization / SMTP / Finish steps. The
  // wizard itself checks `setupCompleted && step === 1` so a user who
  // navigates to /setup on an already-initialized system still gets
  // redirected out; anyone who is actively advancing through steps 2+ is
  // left alone.

  return <>{children}</>;
};

export default SetupGate;
