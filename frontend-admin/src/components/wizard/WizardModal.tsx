import OrkestraCloseButton from 'components/common/OrkestraCloseButton';
import Flex from 'components/common/Flex';
import Lottie from 'lottie-react';

import { Modal } from 'react-bootstrap';
import animationData from './lottie/warning-light.json';

interface WizardModalProps {
  modal: boolean;
  setModal: (value: boolean) => void;
}

const WizardModal = ({ modal, setModal }: WizardModalProps) => {
  return (
    <Modal show={modal} centered dialogClassName="wizard-modal">
      <Modal.Body className="p-4">
        <OrkestraCloseButton
          size="sm"
          className="position-absolute top-0 end-0 me-2 mt-2"
          onClick={() => setModal(!modal)}
        />
        <Flex justifyContent="center" alignItems="center">
          <Lottie
            animationData={animationData}
            loop={true}
            style={{ width: '100px' }}
          />
          <p className="mb-0 flex-1">
            You don't have access to <br />
            the link. Please try again.
          </p>
        </Flex>
      </Modal.Body>
    </Modal>
  );
};

export default WizardModal;
