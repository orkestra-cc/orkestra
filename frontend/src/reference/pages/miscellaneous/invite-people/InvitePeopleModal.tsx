import React, { useState, useEffect, useRef } from 'react';
import { Form, Modal } from 'react-bootstrap';

// Types for Invite People Modal
interface InvitePeopleModalProps {
  show: boolean;
  setShow: (show: boolean) => void;
}

const InvitePeopleModal: React.FC<InvitePeopleModalProps> = ({ show, setShow }) => {
  const [copyLinkText] = useState('https://falcon.com/invited');
  const copyRef = useRef<HTMLInputElement>(null);

  useEffect(() => {
    if (show) {
      copyRef.current?.select();
    }
  }, [show]);
  return (
    <Modal
      show={show}
      onHide={() => setShow(false)}
      contentClassName="overflow-hidden"
    >
      <Modal.Header closeButton>
        <Modal.Title as="h5" id="copyLinkModalLabel">
          Your personal referral link
        </Modal.Title>
      </Modal.Header>
      <Modal.Body className="bg-body-tertiary p-4">
        <Form>
          <Form.Control
            size="sm"
            type="text"
            className="invitation-link"
            defaultValue={copyLinkText}
            ref={copyRef}
          />
        </Form>
      </Modal.Body>
    </Modal>
  );
};

export default InvitePeopleModal;
