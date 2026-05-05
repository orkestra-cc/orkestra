import { useState, useEffect } from 'react';
import { useParams, useNavigate } from 'react-router';
import { Card, Button, Alert, Row, Col, Spinner } from 'react-bootstrap';
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome';
import { faArrowLeft } from '@fortawesome/free-solid-svg-icons';
import { useGetCompanyLookupQuery } from 'store/api/companyApi';
import SubtleBadge from 'components/common/SubtleBadge';
import type { BadgeColor } from 'components/common/SubtleBadge';
import { ACTIVITY_STATUS_COLORS, ACTIVITY_STATUS_LABELS } from 'types/company';
import type { CompanyLookup } from 'types/company';
import { formatItalianDate } from 'types/billing';
import { EnrichmentPanel } from './CompanyEnrichment';

const CompanyDetail = () => {
  const { companyId } = useParams<{ companyId: string }>();
  const navigate = useNavigate();

  const {
    data: company,
    isLoading,
    error,
  } = useGetCompanyLookupQuery(companyId!, { skip: !companyId });

  // Local state so enrichment updates are reflected immediately
  const [displayResult, setDisplayResult] = useState<CompanyLookup | null>(null);

  useEffect(() => {
    if (company) setDisplayResult(company);
  }, [company]);

  if (isLoading) {
    return (
      <div className="text-center py-5">
        <Spinner animation="border" />
      </div>
    );
  }

  if (error) {
    const is404 = 'status' in error && error.status === 404;
    return (
      <Alert variant="warning" className="mt-3">
        {is404
          ? 'Azienda non trovata.'
          : 'Errore durante il caricamento. Riprova più tardi.'}
        <div className="mt-2">
          <Button variant="outline-secondary" size="sm" onClick={() => navigate('/company/lookup')}>
            <FontAwesomeIcon icon={faArrowLeft} className="me-1" />
            Torna alla lista
          </Button>
        </div>
      </Alert>
    );
  }

  if (!displayResult) return null;

  const result = displayResult;

  return (
    <>
      {/* Back button */}
      <div className="mb-3">
        <Button variant="link" className="text-decoration-none p-0" onClick={() => navigate('/company/lookup')}>
          <FontAwesomeIcon icon={faArrowLeft} className="me-1" />
          Ricerca Aziende
        </Button>
      </div>

      {/* Header card */}
      <Card className="mb-3">
        <Card.Body>
          <div className="d-flex align-items-center">
            <h4 className="mb-0 me-2">{result.companyName}</h4>
            <SubtleBadge
              bg={(ACTIVITY_STATUS_COLORS[result.activityStatus] || 'secondary') as BadgeColor}
            >
              {ACTIVITY_STATUS_LABELS[result.activityStatus] || result.activityStatus}
            </SubtleBadge>
          </div>
        </Card.Body>
      </Card>

      {/* Info card */}
      <Card className="mb-3">
        <Card.Header>
          <h6 className="mb-0">Informazioni</h6>
        </Card.Header>
        <Card.Body>
          <Row className="g-3">
            <Col sm={6} md={4}>
              <div className="mb-2">
                <small className="text-muted d-block">Codice Fiscale</small>
                <span className="font-monospace fw-semibold">{result.taxCode}</span>
              </div>
            </Col>
            <Col sm={6} md={4}>
              <div className="mb-2">
                <small className="text-muted d-block">Partita IVA</small>
                <span className="font-monospace fw-semibold">{result.vatCode}</span>
              </div>
            </Col>
            <Col sm={6} md={4}>
              <div className="mb-2">
                <small className="text-muted d-block">Codice SDI</small>
                <span className="font-monospace fw-semibold">{result.sdiCode || '-'}</span>
              </div>
            </Col>
            <Col sm={6} md={4}>
              <div className="mb-2">
                <small className="text-muted d-block">Indirizzo</small>
                <span>
                  {result.address.street}
                  {result.address.streetNumber ? ` ${result.address.streetNumber}` : ''}
                </span>
              </div>
            </Col>
            <Col sm={6} md={4}>
              <div className="mb-2">
                <small className="text-muted d-block">Sede</small>
                <span>
                  {result.address.zipCode} {result.address.town}
                  {result.address.province ? ` (${result.address.province})` : ''}
                </span>
              </div>
            </Col>
            <Col sm={6} md={4}>
              <div className="mb-2">
                <small className="text-muted d-block">Data Registrazione</small>
                <span>{result.registrationDate ? formatItalianDate(result.registrationDate) : '-'}</span>
              </div>
            </Col>
          </Row>
        </Card.Body>
      </Card>

      {/* Enrichment card */}
      <Card>
        <Card.Header>
          <h6 className="mb-0">Arricchimento Dati</h6>
        </Card.Header>
        <Card.Body>
          <EnrichmentPanel
            company={result}
            onEnriched={setDisplayResult}
          />
        </Card.Body>
      </Card>
    </>
  );
};

export default CompanyDetail;
