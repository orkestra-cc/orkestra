
import { Card } from 'react-bootstrap';
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome';
import { Link } from 'react-router';
import { faHome } from '@fortawesome/free-solid-svg-icons';

const Error404 = () => {
  return (
    <Card className="text-center">
      <Card.Body className="p-5">
        <div className="display-1 text-300 fs-error">404</div>
        <p className="lead mt-4 text-800 font-sans-serif fw-semibold">
          The page you are looking for is not found.
        </p>
        <hr />
        <p>
          Make sure the address is correct and that the page has not moved. If
          you think this is an error,
          <a href="mailto:info@exmaple.com" className="ms-1">
            contact us
          </a>
          .
        </p>
        <Link className="btn btn-primary btn-sm mt-3" to="/">
          <FontAwesomeIcon icon={faHome} className="me-2" />
          Go to home
        </Link>
      </Card.Body>
    </Card>
  );
};

export default Error404;
