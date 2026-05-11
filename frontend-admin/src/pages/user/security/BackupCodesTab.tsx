import { useState } from 'react';
import { Alert, Button, Card, Modal, Spinner } from 'react-bootstrap';
import { useGetSelfAuthMethodsQuery } from 'store/api/authApi';
import { useRegenerateBackupCodesMutation } from 'store/api/mfaApi';
import BackupCodesDisplay from './BackupCodesDisplay';

// BackupCodesTab shows how many backup codes the user has left and
// exposes a "Regenerate" affordance. Regenerating destroys the old
// list immediately — the request is gated server-side by
// RequireStepUp(5m), and the global StepUpModal handles the replay
// when the freshness gate fires.
const BackupCodesTab = () => {
  const { data, isLoading } = useGetSelfAuthMethodsQuery();
  const [regenerate, { isLoading: regenerating }] = useRegenerateBackupCodesMutation();
  const [showConfirm, setShowConfirm] = useState(false);
  const [freshCodes, setFreshCodes] = useState<string[] | null>(null);
  const [error, setError] = useState<string | null>(null);

  const totp = data?.mfaFactors.find((f) => f.type === 'totp');
  const remaining = totp?.backupCodesRemaining ?? 0;
  const enrolled = !!totp;

  const onConfirm = async () => {
    setError(null);
    try {
      const res = await regenerate().unwrap();
      setFreshCodes(res.codes);
      setShowConfirm(false);
    } catch (err: unknown) {
      const e = err as { data?: { detail?: string; title?: string; code?: string } };
      if (e?.data?.code === 'step_up_required') {
        setShowConfirm(false);
        return;
      }
      setError(
        e?.data?.detail || e?.data?.title || 'Failed to regenerate backup codes.',
      );
    }
  };

  if (isLoading) {
    return (
      <div className="text-center py-4">
        <Spinner animation="border" size="sm" />
      </div>
    );
  }

  return (
    <>
      <Card className="shadow-none border">
        <Card.Header>
          <Card.Title as="h5" className="mb-0">
            Backup codes
          </Card.Title>
        </Card.Header>
        <Card.Body>
          {!enrolled ? (
            <Alert variant="info" className="fs-10 mb-0">
              Enroll an authenticator app first — backup codes are issued at
              enrollment and can only be regenerated for an enrolled factor.
            </Alert>
          ) : (
            <>
              {error && (
                <Alert variant="danger" className="fs-10">
                  {error}
                </Alert>
              )}
              {freshCodes ? (
                <BackupCodesDisplay
                  codes={freshCodes}
                  ackRequired
                  onDone={() => setFreshCodes(null)}
                  heading="New backup codes"
                />
              ) : (
                <>
                  <p className="fs-10 mb-3">
                    You have <strong>{remaining}</strong> unused backup code
                    {remaining === 1 ? '' : 's'}. Each code works exactly once
                    and is meant for emergency sign-in if you lose your
                    authenticator.
                  </p>
                  <Button
                    variant="outline-primary"
                    onClick={() => setShowConfirm(true)}
                    disabled={regenerating}
                  >
                    Regenerate backup codes
                  </Button>
                </>
              )}
            </>
          )}
        </Card.Body>
      </Card>

      <Modal show={showConfirm} onHide={() => setShowConfirm(false)} centered>
        <Modal.Header closeButton>
          <Modal.Title>Regenerate backup codes</Modal.Title>
        </Modal.Header>
        <Modal.Body>
          Your existing backup codes will stop working immediately. Make sure
          you save the new codes — they are shown exactly once.
        </Modal.Body>
        <Modal.Footer>
          <Button variant="secondary" onClick={() => setShowConfirm(false)}>
            Cancel
          </Button>
          <Button variant="primary" onClick={onConfirm} disabled={regenerating}>
            {regenerating ? 'Regenerating…' : 'Regenerate'}
          </Button>
        </Modal.Footer>
      </Modal>
    </>
  );
};

export default BackupCodesTab;
