import { FontAwesomeIcon } from '@fortawesome/react-fontawesome';
import Flex from 'components/common/Flex';
import { Button, Card } from 'react-bootstrap';
import { useAppContext } from 'providers/AppProvider';

interface InboxFooterProps {
  totalItems: number;
  from: number;
  to: number;
  canNextPage: boolean;
  canPreviousPage: boolean;
  nextPage: () => void;
  prevPage: () => void;
}

const InboxFooter = ({
  totalItems,
  from,
  to,
  canNextPage,
  canPreviousPage,
  nextPage,
  prevPage
}: InboxFooterProps) => {
  const {
    config: { isRTL }
  } = useAppContext();

  return (
    <Card.Footer as={Flex} justifyContent="between" alignItems="center">
      <small>
        2.29 GB <span className="d-none d-sm-inline-block">(13%) </span> of 17
        GB used
      </small>
      <div>
        <small>
          {from}-{to} of {totalItems}
        </small>
        <Button
          variant="falcon-default"
          size="sm"
          className="ms-1 ms-sm-2"
          disabled={!canPreviousPage}
          onClick={prevPage}
        >
          <FontAwesomeIcon icon={`chevron-${isRTL ? 'right' : 'left'}`} />
        </Button>
        <Button
          variant="falcon-default"
          size="sm"
          className="ms-1 ms-sm-2"
          disabled={!canNextPage}
          onClick={nextPage}
        >
          <FontAwesomeIcon icon={`chevron-${isRTL ? 'left' : 'right'}`} />
        </Button>
      </div>
    </Card.Footer>
  );
};

export default InboxFooter;
