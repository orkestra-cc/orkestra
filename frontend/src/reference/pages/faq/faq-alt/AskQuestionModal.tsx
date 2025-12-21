
import { Button, Form, Modal } from 'react-bootstrap';
import FalconCloseButton from 'components/common/FalconCloseButton';
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome';

// Types for Ask Question Modal
interface AskQuestionModalProps {
  show: boolean;
  handleClose: () => void;
}

const AskQuestionModal: React.FC<AskQuestionModalProps> = ({ show, handleClose }) => (
  <Modal show={show} onHide={handleClose} contentClassName="border-0">
    <Modal.Header
      className="bg-card-gradient border-bottom-0"
      data-bs-theme="light"
    >
      <Modal.Title as="h5" className="text-white">
        Ask your question
      </Modal.Title>
      <FalconCloseButton noOutline variant="white" onClick={handleClose} />
    </Modal.Header>
    <Modal.Body>
      <Form>
        <Form.Group className="mb-3">
          <Form.Label className="fs-10" htmlFor="name">
            Name
          </Form.Label>
          <Form.Control type="text" id="name" />
        </Form.Group>
        <Form.Group className="mb-3">
          <Form.Label className="fs-10" htmlFor="email">
            Email
          </Form.Label>
          <Form.Control type="email" id="email" />
        </Form.Group>
        <Form.Group className="mb-3">
          <Form.Label className="fs-10" htmlFor="question">
            Question
          </Form.Label>
          <Form.Control as="textarea" rows={4} id="question" />
        </Form.Group>
        <Button variant="primary" size="sm" className="px-4">
          <FontAwesomeIcon icon="paper-plane" className="me-2" />
          Send
        </Button>
      </Form>
    </Modal.Body>
  </Modal>
);

export default AskQuestionModal;
