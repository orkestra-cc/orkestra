import { useAuthWizardContext } from 'providers/AuthWizardProvider';
import Lottie from 'lottie-react';
import { Button, Col, Row } from 'react-bootstrap';
import celebration from './lottie/celebration.json';
import { UseFormReset } from 'react-hook-form';

interface SuccessProps {
  reset: UseFormReset<Record<string, unknown>>;
}

const Success = ({ reset }: SuccessProps) => {
  const { setStep, setUser } = useAuthWizardContext();

  const emptyData = () => {
    setStep(1);
    setUser({});
    reset();
  };

  return (
    <>
      <Row>
        <Col className="text-center">
          <div className="wizard-lottie-wrapper">
            <div className="wizard-lottie mx-auto">
              <Lottie animationData={celebration} loop={true} />
            </div>
          </div>
          <h4 className="mb-1">Your account is all set!</h4>
          <p className="fs-9">Now you can access to your account</p>
          <Button color="primary" className="px-5 my-3" onClick={emptyData}>
            Start Over
          </Button>
        </Col>
      </Row>
    </>
  );
};

export default Success;
