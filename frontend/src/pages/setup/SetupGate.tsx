import { ReactNode } from 'react';
import { Alert, Button } from 'react-bootstrap';
import { Navigate, useLocation } from 'react-router-dom';
import FalconLoader from 'components/common/FalconLoader';
import { useGetSetupStatusQuery } from 'store/api/setupApi';

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
  const { data, isLoading, isError, error, refetch } = useGetSetupStatusQuery();

  if (isLoading) {
    return <FalconLoader />;
  }

  if (isError || !data) {
    const detail =
      (error as { data?: { detail?: string }; status?: number } | undefined)?.data?.detail ||
      'The setup probe at /v1/setup/status did not respond.';
    return (
      <div className="container py-6" style={{ maxWidth: 640 }}>
        <Alert variant="danger">
          <Alert.Heading>Cannot reach the Orkestra backend</Alert.Heading>
          <p className="mb-2">
            The frontend could not contact the backend to check whether the
            initial setup wizard should run. Make sure the backend container
            is up and reachable from your browser, then retry.
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

  // If setup is complete but someone navigates to /setup directly, punt
  // them to the dashboard — the wizard itself also renders a redirect,
  // but catching it here avoids the flash of the wizard Card.
  if (data.setupCompleted && isSetupPath) {
    return <Navigate to="/dashboard/analytics" replace />;
  }

  return <>{children}</>;
};

export default SetupGate;
