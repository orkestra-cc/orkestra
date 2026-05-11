import { useState } from 'react';
import {
  Card,
  Form,
  Button,
  Row,
  Col,
  Collapse,
  Alert,
  Spinner,
} from 'react-bootstrap';
import { FaChevronDown, FaChevronRight } from 'react-icons/fa';
import { useLazySearchCompaniesQuery } from 'store/api/companyApi';
import type { CompanySearchApiParams, CompanySearchResult } from 'types/company';

interface CompanySearchFiltersProps {
  onResults: (result: CompanySearchResult | null) => void;
}

const CompanySearchFilters = ({ onResults }: CompanySearchFiltersProps) => {
  // Main filters
  const [companyName, setCompanyName] = useState('');
  const [province, setProvince] = useState('');
  const [activityStatus, setActivityStatus] = useState('');

  // Industry & registry
  const [atecoCode, setAtecoCode] = useState('');
  const [cciaa, setCciaa] = useState('');
  const [reaCode, setReaCode] = useState('');
  const [legalFormCode, setLegalFormCode] = useState('');
  const [sdiCode, setSdiCode] = useState('');
  const [pec, setPec] = useState('');

  // Financial
  const [minTurnover, setMinTurnover] = useState('');
  const [maxTurnover, setMaxTurnover] = useState('');
  const [minEmployees, setMinEmployees] = useState('');
  const [maxEmployees, setMaxEmployees] = useState('');

  // Geo
  const [lat, setLat] = useState('');
  const [long, setLong] = useState('');
  const [radius, setRadius] = useState('');
  const [townCode, setTownCode] = useState('');

  // Advanced
  const [shareHolderTaxCode, setShareHolderTaxCode] = useState('');
  const [dataEnrichment, setDataEnrichment] = useState('');
  const [dryRun, setDryRun] = useState(false);

  // Collapsible sections
  const [showIndustry, setShowIndustry] = useState(false);
  const [showFinancial, setShowFinancial] = useState(false);
  const [showGeo, setShowGeo] = useState(false);
  const [showAdvanced, setShowAdvanced] = useState(false);

  const [triggerSearch, { isFetching, error }] = useLazySearchCompaniesQuery();

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();

    const params: CompanySearchApiParams = {};
    if (companyName.trim()) params.companyName = companyName.trim();
    if (province.trim()) params.province = province.trim().toUpperCase();
    if (activityStatus) params.activityStatus = activityStatus;
    if (atecoCode.trim()) params.atecoCode = atecoCode.trim();
    if (cciaa.trim()) params.cciaa = cciaa.trim();
    if (reaCode.trim()) params.reaCode = reaCode.trim();
    if (legalFormCode.trim()) params.legalFormCode = legalFormCode.trim();
    if (sdiCode.trim()) params.sdiCode = sdiCode.trim();
    if (pec.trim()) params.pec = pec.trim();
    if (minTurnover) params.minTurnover = Number(minTurnover);
    if (maxTurnover) params.maxTurnover = Number(maxTurnover);
    if (minEmployees) params.minEmployees = Number(minEmployees);
    if (maxEmployees) params.maxEmployees = Number(maxEmployees);
    if (lat) params.lat = Number(lat);
    if (long) params.long = Number(long);
    if (radius) params.radius = Number(radius);
    if (townCode.trim()) params.townCode = townCode.trim();
    if (shareHolderTaxCode.trim()) params.shareHolderTaxCode = shareHolderTaxCode.trim();
    if (dataEnrichment) params.dataEnrichment = dataEnrichment;
    if (dryRun) params.dryRun = 1;

    // Need at least one filter
    if (Object.keys(params).length === 0 || (Object.keys(params).length === 1 && params.dryRun)) {
      return;
    }

    try {
      const result = await triggerSearch(params).unwrap();
      onResults(result);
    } catch {
      onResults(null);
    }
  };

  const handleReset = () => {
    setCompanyName('');
    setProvince('');
    setActivityStatus('');
    setAtecoCode('');
    setCciaa('');
    setReaCode('');
    setLegalFormCode('');
    setSdiCode('');
    setPec('');
    setMinTurnover('');
    setMaxTurnover('');
    setMinEmployees('');
    setMaxEmployees('');
    setLat('');
    setLong('');
    setRadius('');
    setTownCode('');
    setShareHolderTaxCode('');
    setDataEnrichment('');
    setDryRun(false);
    onResults(null);
  };

  const hasFilters = companyName || province || activityStatus || atecoCode || cciaa ||
    reaCode || legalFormCode || sdiCode || pec || minTurnover || maxTurnover ||
    minEmployees || maxEmployees || lat || long || radius || townCode ||
    shareHolderTaxCode || dataEnrichment;

  const SectionToggle = ({
    label,
    open,
    onToggle,
  }: {
    label: string;
    open: boolean;
    onToggle: () => void;
  }) => (
    <div
      className="d-flex align-items-center cursor-pointer py-2"
      onClick={onToggle}
      role="button"
    >
      {open ? (
        <FaChevronDown className="me-2 text-primary" size={12} />
      ) : (
        <FaChevronRight className="me-2 text-muted" size={12} />
      )}
      <h6 className="mb-0 text-700">{label}</h6>
    </div>
  );

  return (
    <Card>
      <Card.Header>
        <h6 className="mb-0">Filtri di Ricerca</h6>
      </Card.Header>
      <Card.Body>
        <Form onSubmit={handleSubmit}>
          {/* Main filters - always visible */}
          <Row className="g-3 mb-3">
            <Col sm={6} md={4}>
              <Form.Group>
                <Form.Label className="fs-9">Nome Azienda</Form.Label>
                <Form.Control
                  type="text"
                  placeholder="Es. Mario Rossi SRL"
                  value={companyName}
                  onChange={(e) => setCompanyName(e.target.value)}
                />
              </Form.Group>
            </Col>
            <Col sm={6} md={4}>
              <Form.Group>
                <Form.Label className="fs-9">Provincia</Form.Label>
                <Form.Control
                  type="text"
                  placeholder="Es. RM, MI"
                  value={province}
                  onChange={(e) => setProvince(e.target.value)}
                  maxLength={2}
                  className="text-uppercase"
                />
              </Form.Group>
            </Col>
            <Col sm={6} md={4}>
              <Form.Group>
                <Form.Label className="fs-9">Stato Attività</Form.Label>
                <Form.Select
                  value={activityStatus}
                  onChange={(e) => setActivityStatus(e.target.value)}
                >
                  <option value="">Tutti</option>
                  <option value="ATTIVA">Attiva</option>
                  <option value="CESSATA">Cessata</option>
                  <option value="SOSPESA">Sospesa</option>
                </Form.Select>
              </Form.Group>
            </Col>
          </Row>

          {/* Industry & Registry */}
          <hr className="my-2" />
          <SectionToggle
            label="Industria & Registro"
            open={showIndustry}
            onToggle={() => setShowIndustry(!showIndustry)}
          />
          <Collapse in={showIndustry}>
            <div>
              <Row className="g-3 mb-3">
                <Col sm={6} md={4}>
                  <Form.Group>
                    <Form.Label className="fs-9">Codice ATECO</Form.Label>
                    <Form.Control
                      type="text"
                      placeholder="Es. 62.01"
                      value={atecoCode}
                      onChange={(e) => setAtecoCode(e.target.value)}
                    />
                  </Form.Group>
                </Col>
                <Col sm={6} md={4}>
                  <Form.Group>
                    <Form.Label className="fs-9">CCIAA</Form.Label>
                    <Form.Control
                      type="text"
                      placeholder="Es. RM"
                      value={cciaa}
                      onChange={(e) => setCciaa(e.target.value)}
                    />
                  </Form.Group>
                </Col>
                <Col sm={6} md={4}>
                  <Form.Group>
                    <Form.Label className="fs-9">Codice REA</Form.Label>
                    <Form.Control
                      type="text"
                      placeholder="Es. 1234567"
                      value={reaCode}
                      onChange={(e) => setReaCode(e.target.value)}
                    />
                  </Form.Group>
                </Col>
                <Col sm={6} md={4}>
                  <Form.Group>
                    <Form.Label className="fs-9">Forma Giuridica</Form.Label>
                    <Form.Control
                      type="text"
                      placeholder="Es. SR"
                      value={legalFormCode}
                      onChange={(e) => setLegalFormCode(e.target.value)}
                    />
                  </Form.Group>
                </Col>
                <Col sm={6} md={4}>
                  <Form.Group>
                    <Form.Label className="fs-9">Codice SDI</Form.Label>
                    <Form.Control
                      type="text"
                      placeholder="Es. M5UXCR1"
                      value={sdiCode}
                      onChange={(e) => setSdiCode(e.target.value)}
                      className="font-monospace"
                    />
                  </Form.Group>
                </Col>
                <Col sm={6} md={4}>
                  <Form.Group>
                    <Form.Label className="fs-9">PEC</Form.Label>
                    <Form.Control
                      type="email"
                      placeholder="Es. azienda@pec.it"
                      value={pec}
                      onChange={(e) => setPec(e.target.value)}
                    />
                  </Form.Group>
                </Col>
              </Row>
            </div>
          </Collapse>

          {/* Financial */}
          <hr className="my-2" />
          <SectionToggle
            label="Dati Finanziari"
            open={showFinancial}
            onToggle={() => setShowFinancial(!showFinancial)}
          />
          <Collapse in={showFinancial}>
            <div>
              <Row className="g-3 mb-3">
                <Col sm={6} md={3}>
                  <Form.Group>
                    <Form.Label className="fs-9">Fatturato Min (€)</Form.Label>
                    <Form.Control
                      type="number"
                      placeholder="0"
                      value={minTurnover}
                      onChange={(e) => setMinTurnover(e.target.value)}
                    />
                  </Form.Group>
                </Col>
                <Col sm={6} md={3}>
                  <Form.Group>
                    <Form.Label className="fs-9">Fatturato Max (€)</Form.Label>
                    <Form.Control
                      type="number"
                      placeholder="∞"
                      value={maxTurnover}
                      onChange={(e) => setMaxTurnover(e.target.value)}
                    />
                  </Form.Group>
                </Col>
                <Col sm={6} md={3}>
                  <Form.Group>
                    <Form.Label className="fs-9">Dipendenti Min</Form.Label>
                    <Form.Control
                      type="number"
                      placeholder="0"
                      value={minEmployees}
                      onChange={(e) => setMinEmployees(e.target.value)}
                    />
                  </Form.Group>
                </Col>
                <Col sm={6} md={3}>
                  <Form.Group>
                    <Form.Label className="fs-9">Dipendenti Max</Form.Label>
                    <Form.Control
                      type="number"
                      placeholder="∞"
                      value={maxEmployees}
                      onChange={(e) => setMaxEmployees(e.target.value)}
                    />
                  </Form.Group>
                </Col>
              </Row>
            </div>
          </Collapse>

          {/* Geo */}
          <hr className="my-2" />
          <SectionToggle
            label="Geolocalizzazione"
            open={showGeo}
            onToggle={() => setShowGeo(!showGeo)}
          />
          <Collapse in={showGeo}>
            <div>
              <Row className="g-3 mb-3">
                <Col sm={6} md={3}>
                  <Form.Group>
                    <Form.Label className="fs-9">Latitudine</Form.Label>
                    <Form.Control
                      type="number"
                      step="any"
                      placeholder="Es. 41.9028"
                      value={lat}
                      onChange={(e) => setLat(e.target.value)}
                    />
                  </Form.Group>
                </Col>
                <Col sm={6} md={3}>
                  <Form.Group>
                    <Form.Label className="fs-9">Longitudine</Form.Label>
                    <Form.Control
                      type="number"
                      step="any"
                      placeholder="Es. 12.4964"
                      value={long}
                      onChange={(e) => setLong(e.target.value)}
                    />
                  </Form.Group>
                </Col>
                <Col sm={6} md={3}>
                  <Form.Group>
                    <Form.Label className="fs-9">Raggio (km)</Form.Label>
                    <Form.Control
                      type="number"
                      placeholder="Es. 50"
                      value={radius}
                      onChange={(e) => setRadius(e.target.value)}
                    />
                  </Form.Group>
                </Col>
                <Col sm={6} md={3}>
                  <Form.Group>
                    <Form.Label className="fs-9">Codice Comune</Form.Label>
                    <Form.Control
                      type="text"
                      placeholder="Es. H501"
                      value={townCode}
                      onChange={(e) => setTownCode(e.target.value)}
                    />
                  </Form.Group>
                </Col>
              </Row>
            </div>
          </Collapse>

          {/* Advanced */}
          <hr className="my-2" />
          <SectionToggle
            label="Avanzato"
            open={showAdvanced}
            onToggle={() => setShowAdvanced(!showAdvanced)}
          />
          <Collapse in={showAdvanced}>
            <div>
              <Row className="g-3 mb-3">
                <Col sm={6} md={4}>
                  <Form.Group>
                    <Form.Label className="fs-9">CF Socio</Form.Label>
                    <Form.Control
                      type="text"
                      placeholder="Codice Fiscale socio"
                      value={shareHolderTaxCode}
                      onChange={(e) => setShareHolderTaxCode(e.target.value.toUpperCase())}
                      className="font-monospace"
                      maxLength={16}
                    />
                  </Form.Group>
                </Col>
                <Col sm={6} md={4}>
                  <Form.Group>
                    <Form.Label className="fs-9">Livello Arricchimento</Form.Label>
                    <Form.Select
                      value={dataEnrichment}
                      onChange={(e) => setDataEnrichment(e.target.value)}
                    >
                      <option value="">Default (start)</option>
                      <option value="name">Nome</option>
                      <option value="start">Start</option>
                      <option value="advanced">Avanzato</option>
                      <option value="pec">PEC</option>
                      <option value="address">Indirizzo</option>
                      <option value="shareholders">Soci</option>
                    </Form.Select>
                  </Form.Group>
                </Col>
                <Col sm={6} md={4} className="d-flex align-items-end">
                  <Form.Check
                    type="checkbox"
                    id="dryRun"
                    label="Dry Run (solo conteggio)"
                    checked={dryRun}
                    onChange={(e) => setDryRun(e.target.checked)}
                  />
                </Col>
              </Row>
            </div>
          </Collapse>

          {/* Actions */}
          <hr className="my-3" />
          <div className="d-flex gap-2">
            <Button
              type="submit"
              variant="primary"
              disabled={!hasFilters || isFetching}
            >
              {isFetching ? (
                <>
                  <Spinner size="sm" className="me-2" />
                  Ricerca...
                </>
              ) : (
                'Cerca'
              )}
            </Button>
            <Button
              type="button"
              variant="outline-secondary"
              onClick={handleReset}
              disabled={isFetching}
            >
              Reset
            </Button>
          </div>
        </Form>

        {error && (
          <Alert variant="warning" className="mt-3 mb-0">
            Errore durante la ricerca. Riprova più tardi.
          </Alert>
        )}
      </Card.Body>
    </Card>
  );
};

export default CompanySearchFilters;
