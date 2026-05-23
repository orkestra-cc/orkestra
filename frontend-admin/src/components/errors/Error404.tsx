import { Card } from 'react-bootstrap';
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome';
import { Link } from 'react-router';
import { faHome } from '@fortawesome/free-solid-svg-icons';
import { useTranslation } from 'react-i18next';

const Error404 = () => {
  const { t } = useTranslation();
  return (
    <Card className="text-center">
      <Card.Body className="p-5">
        <div className="display-1 text-300 fs-error">404</div>
        <p className="lead mt-4 text-800 font-sans-serif fw-semibold">
          {t('errors.404.title')}
        </p>
        <hr />
        <p>
          {t('errors.404.detail')}
          <a href="mailto:info@exmaple.com" className="ms-1">
            {t('errors.404.contactUs')}
          </a>
          .
        </p>
        <Link className="btn btn-primary btn-sm mt-3" to="/">
          <FontAwesomeIcon icon={faHome} className="me-2" />
          {t('errors.404.goHome')}
        </Link>
      </Card.Body>
    </Card>
  );
};

export default Error404;
