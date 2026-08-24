import { Alert, Button } from 'react-bootstrap';
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome';
import { Trans, useTranslation } from 'react-i18next';

interface FinishStepProps {
  smtpConfigured: boolean;
  /** Name of the Tier-1 organization the finalize saga just created. */
  tenantName: string;
  /**
   * Backend-confirmed provisioning mode: true → manual (more Tier-1
   * tenants can be created later), false → single (permanently capped at
   * this one tenant).
   */
  allowAdditional: boolean;
  onFinish: () => void;
}

/**
 * Final step of the setup wizard. No backend call — once the finalize saga
 * completes, `GET /v1/setup/status` reports phase=complete and the
 * SetupGate stops redirecting here. Just a confirmation screen that recaps
 * what the previous steps created (an organization is now always created —
 * there is no "skipped" path) so the operator knows what state they just
 * landed in.
 */
const FinishStep = ({
  smtpConfigured,
  tenantName,
  allowAdditional,
  onFinish
}: FinishStepProps) => {
  const { t } = useTranslation();
  return (
    <div className="text-center">
      <div className="wizard-lottie-wrapper mb-3">
        <FontAwesomeIcon
          icon="check-circle"
          className="text-success"
          style={{ fontSize: '3rem' }}
        />
      </div>
      <h4 className="mb-2">{t('setup.finish.title')}</h4>
      <p className="text-muted mb-2">
        <Trans
          i18nKey="setup.finish.bodyWithOrg"
          values={{ orgName: tenantName }}
          components={{ strong: <strong /> }}
        />
      </p>
      <p className="text-muted mb-4">
        <Trans
          i18nKey={
            allowAdditional
              ? 'setup.finish.modeManual'
              : 'setup.finish.modeSingle'
          }
          values={{ tenantName }}
          components={{ strong: <strong /> }}
        />
      </p>

      {!smtpConfigured && (
        <Alert
          variant="warning"
          className="fs-10 text-start mx-auto"
          style={{ maxWidth: 560 }}
        >
          <Trans
            i18nKey="setup.finish.smtpWarning"
            components={{ strong: <strong />, code: <code /> }}
          />
        </Alert>
      )}

      <div className="d-grid gap-2 d-md-block">
        <Button variant="primary" size="lg" onClick={onFinish}>
          {t('setup.finish.goToDashboard')}
        </Button>
      </div>
    </div>
  );
};

export default FinishStep;
