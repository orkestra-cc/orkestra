import OrkestraCardHeader from 'components/common/OrkestraCardHeader';

import { Button, Card } from 'react-bootstrap';
import { Link } from 'react-router';
import { useTranslation } from 'react-i18next';

const DangerZone: React.FC = () => {
  const { t } = useTranslation();
  return (
    <Card>
      <OrkestraCardHeader title={t('settings.danger.title')} />
      <Card.Body className="bg-body-tertiary">
        <h5 className="mb-0">{t('settings.danger.privacyHeading')}</h5>
        <p className="fs-10">{t('settings.danger.privacyDescription')}</p>
        <Button
          as={Link as any}
          to="/user/privacy"
          variant="orkestra-danger"
          className="w-100"
        >
          {t('settings.danger.managePrivacy')}
        </Button>
      </Card.Body>
    </Card>
  );
};

export default DangerZone;
