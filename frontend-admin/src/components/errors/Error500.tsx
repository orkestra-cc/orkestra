import { Card } from 'react-bootstrap';
import { useTranslation } from 'react-i18next';

const Error500 = () => {
  const { t } = useTranslation();
  return (
    <Card className="text-center h-100">
      <Card.Body className="p-5">
        <div className="display-1 text-300 fs-error">500</div>
        <p className="lead mt-4 text-800 font-sans-serif fw-semibold">
          {t('errors.500.title')}
        </p>
        <hr />
        <p>
          {t('errors.500.detail')}
          <a href="mailto:info@exmaple.com" className="ms-1">
            {t('errors.500.contactUs')}
          </a>
          .
        </p>
      </Card.Body>
    </Card>
  );
};

export default Error500;
