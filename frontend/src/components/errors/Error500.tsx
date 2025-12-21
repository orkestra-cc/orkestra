
import { Card } from 'react-bootstrap';

const Error500 = () => (
  <Card className="text-center h-100">
    <Card.Body className="p-5">
      <div className="display-1 text-300 fs-error">500</div>
      <p className="lead mt-4 text-800 font-sans-serif fw-semibold">
        Whoops, something went wrong!
      </p>
      <hr />
      <p>
        Try refreshing the page, or go back and attempt the action again.
        If the problem persists,
        <a href="mailto:info@exmaple.com" className="ms-1">
          contact us
        </a>
        .
      </p>
    </Card.Body>
  </Card>
);

export default Error500;
