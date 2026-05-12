import { ReactNode, useEffect } from 'react';
import { Alert, Button } from 'react-bootstrap';
import { Navigate, useLocation } from 'react-router-dom';
import OrkestraLoader from 'components/common/OrkestraLoader';
import { useGetSetupStatusQuery } from 'store/api/setupApi';
import { useAppDispatch } from 'store/hooks';
import { resetTenantState } from 'store/slices/tenantSlice';

interface SetupGateProps {
  children: ReactNode;
}

/**
 * Top-level guard that routes a fresh installation into the onboarding
 * wizard. Placed inside App.tsx, outside the auth gate, so that an
 * uninitialized system never leaks any other route.
 *
 * Behavior:
 *  - While the query is in flight: show a splash so nothing renders stale.
 *  - On query error: show a blocking "cannot reach backend" screen. We
 *    do not fall through to children — because the children path hides
 *    ProtectedRoute which would then redirect to /login and obscure the
 *    real problem (backend unreachable).
 *  - setupCompleted = true: render children normally (the common case
 *    after the first install).
 *  - setupCompleted = false: force-redirect anything that isn't already
 *    under /setup to /setup.
 */
const SetupGate = ({ children }: SetupGateProps) => {
  const location = useLocation();
  const dispatch = useAppDispatch();
  const { data, isLoading, isError, error, refetch } = useGetSetupStatusQuery();

  // If the backend reports the install is not yet set up, drop any
  // tenant state left over from a previous session (e.g. a currentOrgId
  // in localStorage from a database that has since been wiped). Otherwise
  // baseApi would attach a stale X-Tenant-ID to wizard requests and the
  // backend's tenant-resolution middleware would 403 them.
  useEffect(() => {
    if (data && !data.setupCompleted) {
      dispatch(resetTenantState());
    }
  }, [data, dispatch]);

  if (isLoading) {
    return <OrkestraLoader />;
  }

  if (isError || !data) {
    const detail =
      (error as { data?: { detail?: string }; status?: number } | undefined)
        ?.data?.detail ||
      'The setup probe at /v1/setup/status did not respond.';
    return (
      <div className="container py-6" style={{ maxWidth: 640 }}>
        <Alert variant="danger">
          <Alert.Heading>Cannot reach the Orkestra backend</Alert.Heading>
          <p className="mb-2">
            The frontend could not contact the backend to check whether the
            initial setup wizard should run. Make sure the backend container is
            up and reachable from your browser, then retry.
          </p>
          <p className="fs-10 text-muted mb-3">{detail}</p>
          <Button variant="outline-danger" size="sm" onClick={() => refetch()}>
            Retry
          </Button>
        </Alert>
      </div>
    );
  }

  const isSetupPath = location.pathname.startsWith('/setup');

  if (!data.setupCompleted && !isSetupPath) {
    return <Navigate to="/setup" replace />;
  }

  // Note: we intentionally do NOT redirect away from /setup when
  // setupCompleted becomes true. The createInitialAdmin mutation
  // invalidates the Setup tag, which makes this query refetch and flip
  // mid-wizard — if we bounced to /dashboard here, the wizard would
  // never get past the Administrator step to the Organization / SMTP /
  // Finish steps. The wizard itself checks `setupCompleted && step === 1`
  // so a user who navigates to /setup on an already-initialized system
  // still gets redirected out; anyone who is actively advancing through
  // steps 2+ is left alone.

  return <>{children}</>;
};

export default SetupGate;
