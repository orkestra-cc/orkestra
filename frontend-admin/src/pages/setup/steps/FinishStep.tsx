import { Alert, Button } from 'react-bootstrap';
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome';
import { Trans, useTranslation } from 'react-i18next';

interface FinishStepProps {
  smtpConfigured: boolean;
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
const FinishStep = ({ smtpConfigured, orgName, onFinish }: FinishStepProps) => {
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
      <p className="text-muted mb-4">
        {orgName ? (
          <Trans
            i18nKey="setup.finish.bodyWithOrg"
            values={{ orgName }}
            components={{ strong: <strong /> }}
          />
        ) : (
          t('setup.finish.bodyWithoutOrg')
        )}
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
