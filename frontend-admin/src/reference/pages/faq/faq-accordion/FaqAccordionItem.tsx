
import { Accordion, Card } from 'react-bootstrap';
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome';
import classNames from 'classnames';
import createMarkup from 'helpers/createMarkup';

// Types for FAQ Accordion Item
interface FAQ {
  id: number;
  title: string;
  description: string;
}

interface FaqAccordionItemProps {
  faq: FAQ;
  isFirst?: boolean;
  isLast?: boolean;
}

const FaqAccordionItem: React.FC<FaqAccordionItemProps> = ({ faq, isFirst, isLast }) => {
  return (
    <Card
      className={classNames('shadow-none', {
        'rounded-bottom-0': isFirst,
        'border-bottom': !isLast,
        'rounded-0': !isLast && !isFirst
      })}
    >
      <Accordion.Item eventKey={faq.id.toString()} className="border-0">
        <Card.Header className="p-0" id={`faqAccordionHeading${faq.id}`}>
          <Accordion.Button className="btn btn-link text-decoration-none d-block w-100 py-2 px-3 border-0 rounded-0 text-start shadow-none">
            <div className="d-flex">
              <FontAwesomeIcon
                icon="caret-right"
                className="accordion-icon me-3 mt-1"
                transform="shrink-2"
              />
              <span className="fw-medium font-sans-serif text-900">
                {faq.title}
              </span>
            </div>
          </Accordion.Button>
        </Card.Header>
        <Accordion.Body className="p-0">
          <Card.Body className="pt-2">
            <div
              className="ps-3"
              dangerouslySetInnerHTML={createMarkup(faq.description)}
            />
          </Card.Body>
        </Accordion.Body>
      </Accordion.Item>
    </Card>
  );
};

export default FaqAccordionItem;
