import { useState, useEffect, ReactNode } from 'react';
import { Card, Nav, ProgressBar, Alert, Button } from 'react-bootstrap';
import { Navigate, useNavigate } from 'react-router-dom';
import classNames from 'classnames';
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome';
import { IconProp } from '@fortawesome/fontawesome-svg-core';
import { Trans, useTranslation } from 'react-i18next';
import IconButton from 'components/common/IconButton';
import Flex from 'components/common/Flex';
import OrkestraLoader from 'components/common/OrkestraLoader';
import { useGetSetupStatusQuery } from 'store/api/setupApi';
import WelcomeStep from './steps/WelcomeStep';
import AdminStep from './steps/AdminStep';
import OrgStep from './steps/OrgStep';
import FinishStep from './steps/FinishStep';

const STEPS: { icon: string; labelKey: string }[] = [
  { icon: 'hand-holding-heart', labelKey: 'setup.wizard.stepWelcome' },
  { icon: 'user-shield', labelKey: 'setup.wizard.stepAdmin' },
  { icon: 'building', labelKey: 'setup.wizard.stepOrg' },
  { icon: 'check', labelKey: 'setup.wizard.stepDone' }
];

const TOTAL = STEPS.length;

/**
 * First-install onboarding wizard. Routed at /setup and served to anyone
 * visiting the frontend while the backend reports setupCompleted=false.
 * Visually mirrors components/wizard/WizardLayout so the look matches the
 * rest of the admin UI, but uses custom step components instead of the
 * template's hardcoded account-creation forms.
 */
const SetupWizard = () => {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const { data: status, isLoading, error, refetch } = useGetSetupStatusQuery();
  const [step, setStep] = useState<number>(1);
  const [adminFullName, setAdminFullName] = useState<string>('');
  const [orgName, setOrgName] = useState<string>('');

  // If the wizard is re-opened after setup is already done, refuse to run it
  // again — the UI shows a "setup already complete" notice instead.
  useEffect(() => {
    // When the user completes step 2, the admin-creation mutation already
    // invalidates the Setup tag and the query refetches automatically.
    // No manual refetch needed here.
  }, [status]);

  if (isLoading) {
    return <OrkestraLoader />;
  }

  if (error && !status) {
    return (
      <div className="container py-6">
        <Alert variant="danger">
          <Alert.Heading>{t('setup.wizard.errorTitle')}</Alert.Heading>
          <p className="mb-2">
            <Trans
              i18nKey="setup.wizard.errorBody"
              components={{ code: <code /> }}
            />
          </p>
          <Button variant="outline-danger" size="sm" onClick={() => refetch()}>
            {t('setup.wizard.retry')}
          </Button>
        </Alert>
      </div>
    );
  }

  // Setup already done — this page is not reachable through the normal gate,
  // so someone hit /setup manually. Show a short notice and a link home.
  if (status?.setupCompleted && step === 1) {
    return <Navigate to="/dashboard/analytics" replace />;
  }

  const handlePrev = () => setStep(s => Math.max(1, s - 1));
  const handleNext = () => setStep(s => Math.min(TOTAL, s + 1));

  const stepContent: ReactNode = (() => {
    switch (step) {
      case 1:
        return <WelcomeStep onNext={handleNext} />;
      case 2:
        return (
          <AdminStep
            onNext={fullName => {
              setAdminFullName(fullName);
              handleNext();
            }}
          />
        );
      case 3:
        return (
          <OrgStep
            adminFullName={adminFullName}
            onNext={createdName => {
              setOrgName(createdName);
              handleNext();
            }}
          />
        );
      case 4:
        return (
          <FinishStep
            smtpConfigured={status?.smtpConfigured ?? false}
            orgName={orgName}
            onFinish={() => navigate('/dashboard/analytics')}
          />
        );
      default:
        return null;
    }
  })();

  return (
    <div className="container py-5">
      <Card className="theme-wizard mb-5">
        <Card.Header className="bg-body-tertiary pb-2">
          <Nav className="justify-content-center">
            {STEPS.map((item, index) => (
              <StepNavItem
                key={item.labelKey}
                index={index + 1}
                step={step}
                icon={item.icon}
                label={t(item.labelKey)}
              />
            ))}
          </Nav>
        </Card.Header>
        <ProgressBar now={(step / TOTAL) * 100} style={{ height: 2 }} />
        <Card.Body className="fw-normal px-md-6 py-4">{stepContent}</Card.Body>
        {step < TOTAL && (
          <Card.Footer className="px-md-6 bg-body-tertiary d-flex">
            <IconButton
              variant="link"
              icon="chevron-left"
              iconAlign="left"
              transform="down-1 shrink-4"
              className={classNames('px-0 fw-semibold', {
                'd-none': step === 1
              })}
              onClick={handlePrev}
            >
              {t('setup.wizard.back')}
            </IconButton>
            {/* The primary advance button is rendered by each step component
                because only the step knows whether its form is valid and
                whether the advance should trigger a network call. */}
          </Card.Footer>
        )}
      </Card>
    </div>
  );
};

interface StepNavItemProps {
  index: number;
  step: number;
  icon: string;
  label: string;
}

const StepNavItem = ({ index, step, icon, label }: StepNavItemProps) => {
  return (
    <Nav.Item>
      <Nav.Link
        className={classNames('fw-semibold', {
          done: step > index,
          active: step === index
        })}
        // Clicks on step indicators are disabled for the setup wizard —
        // steps must be completed in order because each one depends on
        // the previous (the org step needs the admin's access token).
        onClick={e => e.preventDefault()}
      >
        <Flex alignItems="center" justifyContent="center">
          <FontAwesomeIcon icon={icon as IconProp} />
          <span className="d-none d-md-block mt-1 fs-10 ms-2">{label}</span>
        </Flex>
      </Nav.Link>
    </Nav.Item>
  );
};

export default SetupWizard;
