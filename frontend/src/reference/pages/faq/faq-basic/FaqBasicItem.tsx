
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome';
import { Link } from 'react-router';

// Types for FAQ Basic Item
interface FAQ {
  id: number;
  title: string;
  description: string;
}

interface FaqBasicItemProps {
  faq: FAQ;
  isLast?: boolean;
}

const FaqBasicItem: React.FC<FaqBasicItemProps> = ({ faq, isLast }) => {
  return (
    <>
      <h6>
        <Link to="#!">
          {faq.title}
          <FontAwesomeIcon icon="caret-right" className="ms-2" />
        </Link>
      </h6>
      <p className="fs-10 mb-0">{faq.description}</p>
      {!isLast && <hr className="my-3" />}
    </>
  );
};

export default FaqBasicItem;
