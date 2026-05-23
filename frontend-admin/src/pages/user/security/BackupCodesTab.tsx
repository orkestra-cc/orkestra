import { useState } from 'react';
import { Alert, Button, Card, Modal, Spinner } from 'react-bootstrap';
import { Trans, useTranslation } from 'react-i18next';
import { useGetSelfAuthMethodsQuery } from 'store/api/authApi';
import { useRegenerateBackupCodesMutation } from 'store/api/mfaApi';
import BackupCodesDisplay from './BackupCodesDisplay';

// BackupCodesTab shows how many backup codes the user has left and
// exposes a "Regenerate" affordance. Regenerating destroys the old
// list immediately — the request is gated server-side by
// RequireStepUp(5m), and the global StepUpModal handles the replay
// when the freshness gate fires.
const BackupCodesTab = () => {
  const { t } = useTranslation();
  const { data, isLoading } = useGetSelfAuthMethodsQuery();
  const [regenerate, { isLoading: regenerating }] =
    useRegenerateBackupCodesMutation();
  const [showConfirm, setShowConfirm] = useState(false);
  const [freshCodes, setFreshCodes] = useState<string[] | null>(null);
  const [error, setError] = useState<string | null>(null);

  const totp = data?.mfaFactors.find(f => f.type === 'totp');
  const remaining = totp?.backupCodesRemaining ?? 0;
  const enrolled = !!totp;

  const onConfirm = async () => {
    setError(null);
    try {
      const res = await regenerate().unwrap();
      setFreshCodes(res.codes);
      setShowConfirm(false);
    } catch (err: unknown) {
      const e = err as {
        data?: { detail?: string; title?: string; code?: string };
      };
      if (e?.data?.code === 'step_up_required') {
        setShowConfirm(false);
        return;
      }
      setError(
        e?.data?.detail ||
          e?.data?.title ||
          t('userSecurity.backupCodesTab.errorGeneric')
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
            {t('userSecurity.backupCodesTab.title')}
          </Card.Title>
        </Card.Header>
        <Card.Body>
          {!enrolled ? (
            <Alert variant="info" className="fs-10 mb-0">
              {t('userSecurity.backupCodesTab.noEnrollment')}
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
                  heading={t('userSecurity.backupCodesTab.freshHeading')}
                />
              ) : (
                <>
                  <p className="fs-10 mb-3">
                    <Trans
                      i18nKey={
                        remaining === 1
                          ? 'userSecurity.backupCodesTab.remainingOne'
                          : 'userSecurity.backupCodesTab.remainingOther'
                      }
                      values={{ count: remaining }}
                      components={{ strong: <strong /> }}
                    />
                  </p>
                  <Button
                    variant="outline-primary"
                    onClick={() => setShowConfirm(true)}
                    disabled={regenerating}
                  >
                    {t('userSecurity.backupCodesTab.regenerateButton')}
                  </Button>
                </>
              )}
            </>
          )}
        </Card.Body>
      </Card>

      <Modal show={showConfirm} onHide={() => setShowConfirm(false)} centered>
        <Modal.Header closeButton>
          <Modal.Title>
            {t('userSecurity.backupCodesTab.modalTitle')}
          </Modal.Title>
        </Modal.Header>
        <Modal.Body>{t('userSecurity.backupCodesTab.modalBody')}</Modal.Body>
        <Modal.Footer>
          <Button variant="secondary" onClick={() => setShowConfirm(false)}>
            {t('userSecurity.backupCodesTab.modalCancel')}
          </Button>
          <Button variant="primary" onClick={onConfirm} disabled={regenerating}>
            {regenerating
              ? t('userSecurity.backupCodesTab.modalSubmitting')
              : t('userSecurity.backupCodesTab.modalSubmit')}
          </Button>
        </Modal.Footer>
      </Modal>
    </>
  );
};

export default BackupCodesTab;
