import WizardLayout from './WizardLayout';
import AuthWizardProvider from 'providers/AuthWizardProvider';

interface WizardProps {
  variant?: 'pills' | 'tabs';
  validation?: boolean;
  progressBar?: boolean;
}

const Wizard = ({ variant, validation, progressBar }: WizardProps) => {
  return (
    <AuthWizardProvider>
      <WizardLayout
        variant={variant}
        validation={validation}
        progressBar={progressBar}
      />
    </AuthWizardProvider>
  );
};

export default Wizard;
