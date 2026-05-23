import { Col, Row, Alert } from 'react-bootstrap';
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome';
import { Trans, useTranslation } from 'react-i18next';
import PageHeader from 'components/common/PageHeader';
import CompanyTable from './CompanyTable';

const CompanyManagementPage: React.FC = () => {
  const { t } = useTranslation();
  return (
    <>
      <PageHeader
        title={t('billing.companyPage.title')}
        description={t('billing.companyPage.description')}
        className="mb-3"
      />
      <Row className="g-3 mb-3">
        <Col xxl={12}>
          <Alert variant="info" className="d-flex align-items-center">
            <FontAwesomeIcon icon="info-circle" className="me-2" />
            <div>
              <strong>{t('billing.companyPage.alertHeading')}</strong>{' '}
              <Trans
                i18nKey="billing.companyPage.alertBody"
                components={{ em: <em /> }}
              />
            </div>
          </Alert>
        </Col>
      </Row>
      <Row className="g-3">
        <Col xxl={12}>
          <CompanyTable />
        </Col>
      </Row>
    </>
  );
};

export default CompanyManagementPage;
