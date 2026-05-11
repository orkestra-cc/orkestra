import { Alert, Button } from 'react-bootstrap';
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome';

interface FinishStepProps {
  smtpSkipped: boolean;
  orgName: string;
  onFinish: () => void;
}

/**
 * Final step of the setup wizard. No backend call — once an admin exists,
 * `GET /v1/setup/status` reports setupCompleted=true and the SetupGate
 * stops redirecting here. Just a confirmation screen that recaps what
 * the previous steps created so the operator knows what state they just
 * landed in.
 */
const FinishStep = ({ smtpSkipped, orgName, onFinish }: FinishStepProps) => {
  return (
    <div className="text-center">
      <div className="wizard-lottie-wrapper mb-3">
        <FontAwesomeIcon
          icon="check-circle"
          className="text-success"
          style={{ fontSize: '3rem' }}
        />
      </div>
      <h4 className="mb-2">Orkestra is ready</h4>
      <p className="text-muted mb-4">
        Your administrator account has been created, you&apos;re signed in,
        {orgName ? (
          <>
            {' '}
            and the organization <strong>{orgName}</strong> is active. You
            can rename it or create more organizations later.
          </>
        ) : (
          <> and the setup wizard will not appear again.</>
        )}
      </p>

      {smtpSkipped && (
        <Alert variant="warning" className="fs-10 text-start mx-auto" style={{ maxWidth: 560 }}>
          <strong>SMTP is not configured.</strong> Password-reset and
          verification mail will log to the backend stdout instead of being
          delivered. Configure it any time from{' '}
          <code>/admin/modules</code> — the notification module has the same
          fields you just skipped.
        </Alert>
      )}

      <div className="d-grid gap-2 d-md-block">
        <Button variant="primary" size="lg" onClick={onFinish}>
          Go to the dashboard
        </Button>
      </div>
    </div>
  );
};

export default FinishStep;
