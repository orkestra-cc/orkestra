import { faHome } from '@fortawesome/free-solid-svg-icons';
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome';
import { Button, Card } from 'react-bootstrap';
import { useTranslation } from 'react-i18next';
import { Link } from 'react-router';

const Error500 = () => {
  const { t } = useTranslation();
  return (
    <Card className="text-center h-100">
      <Card.Body className="p-5">
        <div className="display-1 text-300 fs-error">500</div>
        <h1 className="lead mt-4 text-800 font-sans-serif fw-semibold">
          {t('errors.500.title')}
        </h1>
        <hr />
        <p>
          {t('errors.500.detail')}
          <a href="mailto:info@exmaple.com" className="ms-1">
            {t('errors.500.contactUs')}
          </a>
          .
        </p>
        <Button
          variant="orkestra-primary"
          size="sm"
          className="mt-3"
          as={Link as any}
          to="/user/dashboard"
        >
          <FontAwesomeIcon icon={faHome} className="me-2" />
          {t('errors.500.backToDashboard')}
        </Button>
      </Card.Body>
    </Card>
  );
};

export default Error500;
