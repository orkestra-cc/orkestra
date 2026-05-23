import OrkestraCardHeader from 'components/common/OrkestraCardHeader';

import { Button, Card } from 'react-bootstrap';
import { Link } from 'react-router';
import { Trans, useTranslation } from 'react-i18next';

const BillingSettings: React.FC = () => {
  const { t } = useTranslation();
  return (
    <Card className="mb-3">
      <OrkestraCardHeader title={t('settings.billing.title')} />
      <Card.Body className="bg-body-tertiary">
        <h5>{t('settings.billing.plan')}</h5>
        <p className="fs-9">
          <Trans
            i18nKey="settings.billing.developerPlan"
            components={{ strong: <strong /> }}
          />
        </p>
        <Button as={Link as any} variant="orkestra-default" size="sm" to="#!">
          {t('settings.billing.updatePlan')}
        </Button>
      </Card.Body>
      <Card.Body className="bg-body-tertiary border-top">
        <h5>{t('settings.billing.payment')}</h5>
        <p className="fs-9">{t('settings.billing.noPayment')}</p>
        <Button as={Link as any} variant="orkestra-default" size="sm" to="#!">
          {t('settings.billing.addPayment')}
        </Button>
      </Card.Body>
    </Card>
  );
};

export default BillingSettings;
