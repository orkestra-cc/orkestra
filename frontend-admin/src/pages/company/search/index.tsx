import { useState } from 'react';
import { Col, Row } from 'react-bootstrap';
import CompanySearchGreetings from './CompanySearchGreetings';
import CompanySearchFilters from './CompanySearchFilters';
import CompanySearchResults from './CompanySearchResults';
import type { CompanySearchResult } from 'types/company';

const CompanySearchPage: React.FC = () => {
  const [result, setResult] = useState<CompanySearchResult | null>(null);

  return (
    <>
      <Row className="g-3 mb-3">
        <Col xxl={12}>
          <CompanySearchGreetings />
        </Col>
      </Row>
      <Row className="g-3 mb-3">
        <Col xxl={12}>
          <CompanySearchFilters onResults={setResult} />
        </Col>
      </Row>
      {result && (
        <Row className="g-3">
          <Col xxl={12}>
            <CompanySearchResults result={result} />
          </Col>
        </Row>
      )}
    </>
  );
};

export default CompanySearchPage;
