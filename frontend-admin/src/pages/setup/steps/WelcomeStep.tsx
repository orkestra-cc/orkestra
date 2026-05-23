import { Button } from 'react-bootstrap';
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome';
import { Trans, useTranslation } from 'react-i18next';

interface WelcomeStepProps {
  onNext: () => void;
}

/**
 * First step of the setup wizard. Introduces the flow and explains what
 * the operator is about to do. Purely informational — no form state.
 */
const WelcomeStep = ({ onNext }: WelcomeStepProps) => {
  const { t } = useTranslation();
  return (
    <div className="text-center">
      <div className="wizard-lottie-wrapper mb-3">
        <FontAwesomeIcon
          icon="rocket"
          className="text-primary"
          style={{ fontSize: '3rem' }}
        />
      </div>
      <h4 className="mb-2">{t('setup.welcome.title')}</h4>
      <p className="text-muted mb-4">{t('setup.welcome.intro')}</p>

      <div className="text-start mx-auto" style={{ maxWidth: 460 }}>
        <ol className="ps-3 mb-4">
          <li className="mb-2">
            <Trans
              i18nKey="setup.welcome.step1"
              components={{ strong: <strong />, code: <code /> }}
            />
          </li>
          <li className="mb-2">
            <Trans
              i18nKey="setup.welcome.step2"
              components={{ strong: <strong /> }}
            />
          </li>
          <li className="mb-2">
            <Trans
              i18nKey="setup.welcome.step3"
              components={{ strong: <strong /> }}
            />
          </li>
          <li>
            <Trans
              i18nKey="setup.welcome.step4"
              components={{ strong: <strong /> }}
            />
          </li>
        </ol>
      </div>

      <div className="d-grid gap-2 d-md-block">
        <Button variant="primary" size="lg" onClick={onNext}>
          {t('setup.welcome.getStarted')}
        </Button>
      </div>
    </div>
  );
};

export default WelcomeStep;
