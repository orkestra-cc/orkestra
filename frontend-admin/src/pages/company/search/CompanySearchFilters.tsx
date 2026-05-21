import { useState } from 'react';
import {
  Card,
  Form,
  Button,
  Row,
  Col,
  Collapse,
  Alert,
  Spinner
} from 'react-bootstrap';
import { FaChevronDown, FaChevronRight } from 'react-icons/fa';
import { useTranslation } from 'react-i18next';
import { useLazySearchCompaniesQuery } from 'store/api/companyApi';
import type {
  CompanySearchApiParams,
  CompanySearchResult
} from 'types/company';

interface CompanySearchFiltersProps {
  onResults: (result: CompanySearchResult | null) => void;
}

const CompanySearchFilters = ({ onResults }: CompanySearchFiltersProps) => {
  const { t } = useTranslation();
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
    if (shareHolderTaxCode.trim())
      params.shareHolderTaxCode = shareHolderTaxCode.trim();
    if (dataEnrichment) params.dataEnrichment = dataEnrichment;
    if (dryRun) params.dryRun = 1;

    // Need at least one filter
    if (
      Object.keys(params).length === 0 ||
      (Object.keys(params).length === 1 && params.dryRun)
    ) {
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

  const hasFilters =
    companyName ||
    province ||
    activityStatus ||
    atecoCode ||
    cciaa ||
    reaCode ||
    legalFormCode ||
    sdiCode ||
    pec ||
    minTurnover ||
    maxTurnover ||
    minEmployees ||
    maxEmployees ||
    lat ||
    long ||
    radius ||
    townCode ||
    shareHolderTaxCode ||
    dataEnrichment;

  const SectionToggle = ({
    label,
    open,
    onToggle
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
        <h6 className="mb-0">{t('company.search.filters.cardTitle')}</h6>
      </Card.Header>
      <Card.Body>
        <Form onSubmit={handleSubmit}>
          {/* Main filters - always visible */}
          <Row className="g-3 mb-3">
            <Col sm={6} md={4}>
              <Form.Group>
                <Form.Label className="fs-9">
                  {t('company.search.filters.labelCompanyName')}
                </Form.Label>
                <Form.Control
                  type="text"
                  placeholder={t(
                    'company.search.filters.placeholderCompanyName'
                  )}
                  value={companyName}
                  onChange={e => setCompanyName(e.target.value)}
                />
              </Form.Group>
            </Col>
            <Col sm={6} md={4}>
              <Form.Group>
                <Form.Label className="fs-9">
                  {t('company.search.filters.labelProvince')}
                </Form.Label>
                <Form.Control
                  type="text"
                  placeholder={t('company.search.filters.placeholderProvince')}
                  value={province}
                  onChange={e => setProvince(e.target.value)}
                  maxLength={2}
                  className="text-uppercase"
                />
              </Form.Group>
            </Col>
            <Col sm={6} md={4}>
              <Form.Group>
                <Form.Label className="fs-9">
                  {t('company.search.filters.labelActivityStatus')}
                </Form.Label>
                <Form.Select
                  value={activityStatus}
                  onChange={e => setActivityStatus(e.target.value)}
                >
                  <option value="">
                    {t('company.search.filters.activityAll')}
                  </option>
                  <option value="ATTIVA">
                    {t('company.search.filters.activityActive')}
                  </option>
                  <option value="CESSATA">
                    {t('company.search.filters.activityCeased')}
                  </option>
                  <option value="SOSPESA">
                    {t('company.search.filters.activitySuspended')}
                  </option>
                </Form.Select>
              </Form.Group>
            </Col>
          </Row>

          {/* Industry & Registry */}
          <hr className="my-2" />
          <SectionToggle
            label={t('company.search.filters.sectionIndustry')}
            open={showIndustry}
            onToggle={() => setShowIndustry(!showIndustry)}
          />
          <Collapse in={showIndustry}>
            <div>
              <Row className="g-3 mb-3">
                <Col sm={6} md={4}>
                  <Form.Group>
                    <Form.Label className="fs-9">
                      {t('company.search.filters.labelAteco')}
                    </Form.Label>
                    <Form.Control
                      type="text"
                      placeholder={t('company.search.filters.placeholderAteco')}
                      value={atecoCode}
                      onChange={e => setAtecoCode(e.target.value)}
                    />
                  </Form.Group>
                </Col>
                <Col sm={6} md={4}>
                  <Form.Group>
                    <Form.Label className="fs-9">
                      {t('company.search.filters.labelCciaa')}
                    </Form.Label>
                    <Form.Control
                      type="text"
                      placeholder={t('company.search.filters.placeholderCciaa')}
                      value={cciaa}
                      onChange={e => setCciaa(e.target.value)}
                    />
                  </Form.Group>
                </Col>
                <Col sm={6} md={4}>
                  <Form.Group>
                    <Form.Label className="fs-9">
                      {t('company.search.filters.labelReaCode')}
                    </Form.Label>
                    <Form.Control
                      type="text"
                      placeholder={t(
                        'company.search.filters.placeholderReaCode'
                      )}
                      value={reaCode}
                      onChange={e => setReaCode(e.target.value)}
                    />
                  </Form.Group>
                </Col>
                <Col sm={6} md={4}>
                  <Form.Group>
                    <Form.Label className="fs-9">
                      {t('company.search.filters.labelLegalForm')}
                    </Form.Label>
                    <Form.Control
                      type="text"
                      placeholder={t(
                        'company.search.filters.placeholderLegalForm'
                      )}
                      value={legalFormCode}
                      onChange={e => setLegalFormCode(e.target.value)}
                    />
                  </Form.Group>
                </Col>
                <Col sm={6} md={4}>
                  <Form.Group>
                    <Form.Label className="fs-9">
                      {t('company.search.filters.labelSdiCode')}
                    </Form.Label>
                    <Form.Control
                      type="text"
                      placeholder={t(
                        'company.search.filters.placeholderSdiCode'
                      )}
                      value={sdiCode}
                      onChange={e => setSdiCode(e.target.value)}
                      className="font-monospace"
                    />
                  </Form.Group>
                </Col>
                <Col sm={6} md={4}>
                  <Form.Group>
                    <Form.Label className="fs-9">
                      {t('company.search.filters.labelPec')}
                    </Form.Label>
                    <Form.Control
                      type="email"
                      placeholder={t('company.search.filters.placeholderPec')}
                      value={pec}
                      onChange={e => setPec(e.target.value)}
                    />
                  </Form.Group>
                </Col>
              </Row>
            </div>
          </Collapse>

          {/* Financial */}
          <hr className="my-2" />
          <SectionToggle
            label={t('company.search.filters.sectionFinancial')}
            open={showFinancial}
            onToggle={() => setShowFinancial(!showFinancial)}
          />
          <Collapse in={showFinancial}>
            <div>
              <Row className="g-3 mb-3">
                <Col sm={6} md={3}>
                  <Form.Group>
                    <Form.Label className="fs-9">
                      {t('company.search.filters.labelMinTurnover')}
                    </Form.Label>
                    <Form.Control
                      type="number"
                      placeholder="0"
                      value={minTurnover}
                      onChange={e => setMinTurnover(e.target.value)}
                    />
                  </Form.Group>
                </Col>
                <Col sm={6} md={3}>
                  <Form.Group>
                    <Form.Label className="fs-9">
                      {t('company.search.filters.labelMaxTurnover')}
                    </Form.Label>
                    <Form.Control
                      type="number"
                      placeholder="∞"
                      value={maxTurnover}
                      onChange={e => setMaxTurnover(e.target.value)}
                    />
                  </Form.Group>
                </Col>
                <Col sm={6} md={3}>
                  <Form.Group>
                    <Form.Label className="fs-9">
                      {t('company.search.filters.labelMinEmployees')}
                    </Form.Label>
                    <Form.Control
                      type="number"
                      placeholder="0"
                      value={minEmployees}
                      onChange={e => setMinEmployees(e.target.value)}
                    />
                  </Form.Group>
                </Col>
                <Col sm={6} md={3}>
                  <Form.Group>
                    <Form.Label className="fs-9">
                      {t('company.search.filters.labelMaxEmployees')}
                    </Form.Label>
                    <Form.Control
                      type="number"
                      placeholder="∞"
                      value={maxEmployees}
                      onChange={e => setMaxEmployees(e.target.value)}
                    />
                  </Form.Group>
                </Col>
              </Row>
            </div>
          </Collapse>

          {/* Geo */}
          <hr className="my-2" />
          <SectionToggle
            label={t('company.search.filters.sectionGeo')}
            open={showGeo}
            onToggle={() => setShowGeo(!showGeo)}
          />
          <Collapse in={showGeo}>
            <div>
              <Row className="g-3 mb-3">
                <Col sm={6} md={3}>
                  <Form.Group>
                    <Form.Label className="fs-9">
                      {t('company.search.filters.labelLatitude')}
                    </Form.Label>
                    <Form.Control
                      type="number"
                      step="any"
                      placeholder={t(
                        'company.search.filters.placeholderLatitude'
                      )}
                      value={lat}
                      onChange={e => setLat(e.target.value)}
                    />
                  </Form.Group>
                </Col>
                <Col sm={6} md={3}>
                  <Form.Group>
                    <Form.Label className="fs-9">
                      {t('company.search.filters.labelLongitude')}
                    </Form.Label>
                    <Form.Control
                      type="number"
                      step="any"
                      placeholder={t(
                        'company.search.filters.placeholderLongitude'
                      )}
                      value={long}
                      onChange={e => setLong(e.target.value)}
                    />
                  </Form.Group>
                </Col>
                <Col sm={6} md={3}>
                  <Form.Group>
                    <Form.Label className="fs-9">
                      {t('company.search.filters.labelRadius')}
                    </Form.Label>
                    <Form.Control
                      type="number"
                      placeholder={t(
                        'company.search.filters.placeholderRadius'
                      )}
                      value={radius}
                      onChange={e => setRadius(e.target.value)}
                    />
                  </Form.Group>
                </Col>
                <Col sm={6} md={3}>
                  <Form.Group>
                    <Form.Label className="fs-9">
                      {t('company.search.filters.labelTownCode')}
                    </Form.Label>
                    <Form.Control
                      type="text"
                      placeholder={t(
                        'company.search.filters.placeholderTownCode'
                      )}
                      value={townCode}
                      onChange={e => setTownCode(e.target.value)}
                    />
                  </Form.Group>
                </Col>
              </Row>
            </div>
          </Collapse>

          {/* Advanced */}
          <hr className="my-2" />
          <SectionToggle
            label={t('company.search.filters.sectionAdvanced')}
            open={showAdvanced}
            onToggle={() => setShowAdvanced(!showAdvanced)}
          />
          <Collapse in={showAdvanced}>
            <div>
              <Row className="g-3 mb-3">
                <Col sm={6} md={4}>
                  <Form.Group>
                    <Form.Label className="fs-9">
                      {t('company.search.filters.labelShareHolderTaxCode')}
                    </Form.Label>
                    <Form.Control
                      type="text"
                      placeholder={t(
                        'company.search.filters.placeholderShareHolderTaxCode'
                      )}
                      value={shareHolderTaxCode}
                      onChange={e =>
                        setShareHolderTaxCode(e.target.value.toUpperCase())
                      }
                      className="font-monospace"
                      maxLength={16}
                    />
                  </Form.Group>
                </Col>
                <Col sm={6} md={4}>
                  <Form.Group>
                    <Form.Label className="fs-9">
                      {t('company.search.filters.labelDataEnrichment')}
                    </Form.Label>
                    <Form.Select
                      value={dataEnrichment}
                      onChange={e => setDataEnrichment(e.target.value)}
                    >
                      <option value="">
                        {t('company.search.filters.enrichmentDefault')}
                      </option>
                      <option value="name">
                        {t('company.search.filters.enrichmentName')}
                      </option>
                      <option value="start">
                        {t('company.search.filters.enrichmentStart')}
                      </option>
                      <option value="advanced">
                        {t('company.search.filters.enrichmentAdvanced')}
                      </option>
                      <option value="pec">
                        {t('company.search.filters.enrichmentPec')}
                      </option>
                      <option value="address">
                        {t('company.search.filters.enrichmentAddress')}
                      </option>
                      <option value="shareholders">
                        {t('company.search.filters.enrichmentShareholders')}
                      </option>
                    </Form.Select>
                  </Form.Group>
                </Col>
                <Col sm={6} md={4} className="d-flex align-items-end">
                  <Form.Check
                    type="checkbox"
                    id="dryRun"
                    label={t('company.search.filters.labelDryRun')}
                    checked={dryRun}
                    onChange={e => setDryRun(e.target.checked)}
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
                  {t('company.search.filters.submitting')}
                </>
              ) : (
                t('company.search.filters.submit')
              )}
            </Button>
            <Button
              type="button"
              variant="outline-secondary"
              onClick={handleReset}
              disabled={isFetching}
            >
              {t('company.search.filters.reset')}
            </Button>
          </div>
        </Form>

        {error && (
          <Alert variant="warning" className="mt-3 mb-0">
            {t('company.search.filters.errorGeneric')}
          </Alert>
        )}
      </Card.Body>
    </Card>
  );
};

export default CompanySearchFilters;
