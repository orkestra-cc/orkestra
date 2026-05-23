import { Col, Row } from 'react-bootstrap';
import { useTranslation } from 'react-i18next';
import AdvanceTableSearchBox from 'components/common/advance-table/AdvanceTableSearchBox';

const CompanyLookupTableHeader = () => {
  const { t } = useTranslation();
  return (
    <div className="d-lg-flex justify-content-between">
      <Row className="flex-between-center gy-2 px-x1">
        <Col xs="auto" className="pe-0">
          <h6 className="mb-0">{t('company.lookup.tableTitle')}</h6>
        </Col>
        <Col xs="auto">
          <AdvanceTableSearchBox
            className="input-search-width"
            placeholder={t('company.lookup.searchPlaceholder')}
          />
        </Col>
      </Row>
    </div>
  );
};

export default CompanyLookupTableHeader;
