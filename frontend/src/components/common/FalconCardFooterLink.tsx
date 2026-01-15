import { Button, Card } from 'react-bootstrap';
import { Link } from 'react-router-dom';
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome';
import classNames from 'classnames';

interface FalconCardFooterLinkProps {
  title: string;
  bg?: string;
  borderTop?: boolean;
  to?: string;
  className?: string;
  [key: string]: any;
}

const FalconCardFooterLink = ({
  title,
  bg = 'body-tertiary',
  borderTop,
  to = '#!',
  className,
  ...rest
}: FalconCardFooterLinkProps) => (
  <Card.Footer
    className={classNames(className, `bg-${bg} p-0`, {
      'border-top': borderTop
    })}
  >
    <Link to={to} className="w-100 py-2">
      <Button variant="link" size="lg" className="w-100 py-2" {...rest}>
        {title}
        <FontAwesomeIcon icon="chevron-right" className="ms-1 fs-11" />
      </Button>
    </Link>
  </Card.Footer>
);

export default FalconCardFooterLink;
