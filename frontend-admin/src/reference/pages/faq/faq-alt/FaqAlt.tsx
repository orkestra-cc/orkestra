import React, { useState } from 'react';
import { Button, Card, Col, Row } from 'react-bootstrap';
import { faqs as faqsData } from 'data/faqs';
import AskQuestionModal from './AskQuestionModal';

interface FAQ {
  id: number;
  title: string;
  description: string;
}

interface FaqAltItemProps {
  faq: FAQ;
}

const FaqAltItem: React.FC<FaqAltItemProps> = ({ faq }) => {
  return (
    <>
      <h5 className="fs-9">{faq.title}</h5>
      <p className="fs-10">{faq.description}</p>
    </>
  );
};

const FaqAlt: React.FC = () => {
  const [faqs] = useState(faqsData);
  const [showModal, setShowModal] = useState(false);

  const handleModalClose = () => setShowModal(false);
  const handleModalShow = () => setShowModal(true);

  return (
    <Card>
      <Card.Header>
        <h5 className="mb-0">Frequently asked questions</h5>
      </Card.Header>
      <Card.Body className="bg-body-tertiary pb-0">
        <Row>
          <Col lg={6}>
            {faqs.slice(0, Math.floor(faqs.length / 2)).map((faq: FAQ) => (
              <FaqAltItem key={faq.id} faq={faq} />
            ))}
          </Col>
          <Col lg={6}>
            {faqs.slice(Math.floor(faqs.length / 2)).map((faq: FAQ) => (
              <FaqAltItem key={faq.id} faq={faq} />
            ))}
          </Col>
        </Row>
      </Card.Body>
      <Card.Footer className="py-3">
        <div className="text-center">
          <h6 className="fs-9 fw-normal">Have more questions?</h6>
          <Button variant="orkestra-primary" size="sm" onClick={handleModalShow}>
            Ask us anything
          </Button>
        </div>

        <AskQuestionModal show={showModal} handleClose={handleModalClose} />
      </Card.Footer>
    </Card>
  );
};

export default FaqAlt;
