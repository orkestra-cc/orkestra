import { Button } from 'react-bootstrap';
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome';

interface WelcomeStepProps {
  onNext: () => void;
}

/**
 * First step of the setup wizard. Introduces the flow and explains what
 * the operator is about to do. Purely informational — no form state.
 */
const WelcomeStep = ({ onNext }: WelcomeStepProps) => {
  return (
    <div className="text-center">
      <div className="wizard-lottie-wrapper mb-3">
        <FontAwesomeIcon
          icon="rocket"
          className="text-primary"
          style={{ fontSize: '3rem' }}
        />
      </div>
      <h4 className="mb-2">Welcome to Orkestra</h4>
      <p className="text-muted mb-4">
        Let&apos;s get your deployment ready. This will take about a minute and
        needs to happen exactly once per install.
      </p>

      <div className="text-start mx-auto" style={{ maxWidth: 440 }}>
        <ol className="ps-3 mb-4">
          <li className="mb-2">
            <strong>Create an administrator account.</strong> The first user
            becomes the root <code>developer</code> and can manage everything
            else from the admin UI.
          </li>
          <li className="mb-2">
            <strong>Configure outbound email.</strong> Password resets and
            verification links need a working SMTP relay. You can skip this
            step and configure it later, but those flows will silently drop
            mail until you do.
          </li>
          <li>
            <strong>You&apos;re done.</strong> The wizard won&apos;t reappear
            on the next boot.
          </li>
        </ol>
      </div>

      <div className="d-grid gap-2 d-md-block">
        <Button variant="primary" size="lg" onClick={onNext}>
          Get started
        </Button>
      </div>
    </div>
  );
};

export default WelcomeStep;
