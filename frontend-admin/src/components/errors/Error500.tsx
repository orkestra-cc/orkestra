import { faHome } from '@fortawesome/free-solid-svg-icons';
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome';
import { Button, Card } from 'react-bootstrap';
import { useTranslation } from 'react-i18next';
import { Link } from 'react-router';

interface Error500Props {
  // Load the dashboard from the server instead of navigating in the app: the
  // route error page uses it, since the app that failed may not recover.
  fullPageLoad?: boolean;
}

const Error500 = ({ fullPageLoad = false }: Error500Props) => {
  const { t } = useTranslation();
  const linkProps = fullPageLoad
    ? { href: `${import.meta.env.BASE_URL}user/dashboard` }
    : { as: Link as any, to: '/user/dashboard' };
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
          {...linkProps}
        >
          <FontAwesomeIcon icon={faHome} className="me-2" />
          {t('errors.500.backToDashboard')}
        </Button>
      </Card.Body>
    </Card>
  );
};

export default Error500;
