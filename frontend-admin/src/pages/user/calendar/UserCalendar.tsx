import { Card, Col, Row } from 'react-bootstrap';
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome';
import { useTranslation } from 'react-i18next';
import PageHeader from 'components/common/PageHeader';

const UserCalendar = () => {
  const { t } = useTranslation();
  return (
    <>
      <PageHeader
        title={t('userScaffold.calendar.pageTitle')}
        description={t('userScaffold.calendar.pageDescription')}
        className="mb-3"
      />
      <Row className="g-3">
        <Col lg={12}>
          <Card>
            <Card.Body className="text-center py-5">
              <FontAwesomeIcon
                icon="calendar-alt"
                className="text-400 mb-3"
                style={{ fontSize: '3rem' }}
              />
              <h4 className="text-700">
                {t('userScaffold.calendar.comingSoonTitle')}
              </h4>
              <p className="text-500 mb-0">
                {t('userScaffold.calendar.comingSoonBody')}
              </p>
            </Card.Body>
          </Card>
        </Col>
      </Row>
    </>
  );
};

export default UserCalendar;
