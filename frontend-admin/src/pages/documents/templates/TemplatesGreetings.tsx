import { Card, Row, Col, Badge } from 'react-bootstrap';
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome';
import {
  faFileAlt,
  faCheck,
  faExclamationTriangle
} from '@fortawesome/free-solid-svg-icons';
import { useTranslation } from 'react-i18next';
import { useGetDocumentsServiceStatusQuery } from '../../../store/api/documentsApi';

const TemplatesGreetings: React.FC = () => {
  const { t } = useTranslation();
  const { data: serviceStatus, isLoading } =
    useGetDocumentsServiceStatusQuery();

  return (
    <Card className="bg-100 shadow-none border mb-3">
      <Card.Body className="py-3">
        <Row className="g-0 align-items-center">
          <Col xs="auto" className="pe-3">
            <div
              className="d-flex align-items-center justify-content-center rounded-circle bg-primary"
              style={{ width: 56, height: 56 }}
            >
              <FontAwesomeIcon icon={faFileAlt} className="text-white fs-4" />
            </div>
          </Col>
          <Col>
            <Row className="align-items-center">
              <Col>
                <h5 className="mb-0 text-primary fw-semi-bold">
                  {t('documents.templates.greetings.title')}
                </h5>
                <p className="mb-0 fs-10 text-600">
                  {t('documents.templates.greetings.subtitle')}
                </p>
              </Col>
              <Col xs="auto">
                {!isLoading && (
                  <Badge
                    bg={serviceStatus?.available ? 'success' : 'warning'}
                    className="d-flex align-items-center gap-1"
                  >
                    <FontAwesomeIcon
                      icon={
                        serviceStatus?.available
                          ? faCheck
                          : faExclamationTriangle
                      }
                      className="fs-11"
                    />
                    {serviceStatus?.available
                      ? t('documents.templates.greetings.serviceActive')
                      : t('documents.templates.greetings.serviceUnavailable')}
                  </Badge>
                )}
              </Col>
            </Row>
          </Col>
        </Row>
      </Card.Body>
    </Card>
  );
};

export default TemplatesGreetings;
