import { Card } from 'react-bootstrap';
import { FaBuilding } from 'react-icons/fa';
import { useTranslation } from 'react-i18next';
import Background from 'components/common/Background';
import greetingsBg from 'assets/img/illustrations/ticket-greetings-bg.png';

function CompanyLookupGreetings() {
  const { t } = useTranslation();
  return (
    <Card className="bg-body-tertiary dark__bg-opacity-50 shadow-none h-100">
      <Background image={greetingsBg} className="bg-card d-none d-sm-block" />
      <Card.Header className="d-flex align-items-center z-1 p-0">
        <div className="bg-primary rounded-circle p-3 ms-3">
          <FaBuilding className="text-white" size={32} />
        </div>
        <div className="ms-3">
          <h6 className="mb-1 text-primary">{t('company.lookup.kicker')}</h6>
          <h4 className="mb-0 text-primary fw-bold">
            {t('company.lookup.title')}
            <span className="text-info fw-medium">
              {t('company.lookup.titleAccent')}
            </span>
          </h4>
        </div>
      </Card.Header>
    </Card>
  );
}

export default CompanyLookupGreetings;
