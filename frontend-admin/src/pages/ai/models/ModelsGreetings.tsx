import { Card } from 'react-bootstrap';
import { FaRobot } from 'react-icons/fa';
import { useTranslation } from 'react-i18next';
import Background from 'components/common/Background';
import greetingsBg from 'assets/img/illustrations/ticket-greetings-bg.png';

function ModelsGreetings() {
  const { t } = useTranslation();
  return (
    <Card className="bg-body-tertiary dark__bg-opacity-50 shadow-none h-100">
      <Background image={greetingsBg} className="bg-card d-none d-sm-block" />
      <Card.Header className="d-flex align-items-center z-1 p-0">
        <div className="bg-primary rounded-circle p-3 ms-3">
          <FaRobot className="text-white" size={32} />
        </div>
        <div className="ms-3">
          <h6 className="mb-1 text-primary">
            {t('aiModels.greetings.kicker')}
          </h6>
          <h4 className="mb-0 text-primary fw-bold">
            {t('aiModels.greetings.title')}
            <span className="text-info fw-medium">
              {t('aiModels.greetings.titleAccent')}
            </span>
          </h4>
          <p className="mb-0 mt-1 text-muted small">
            {t('aiModels.greetings.subtitle')}
          </p>
        </div>
      </Card.Header>
    </Card>
  );
}

export default ModelsGreetings;
