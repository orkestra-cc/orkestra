import React, { useState, useEffect } from 'react';
import {
  Modal,
  Button,
  Form,
  Alert,
  Tab,
  Nav,
  Row,
  Col,
  Spinner
} from 'react-bootstrap';
import {
  useCreateCompanyMutation,
  useUpdateCompanyMutation,
  useLazyGetBusinessRegistryConfigQuery,
  useConfigureBusinessRegistryMutation
} from 'store/api/billingApi';
import type {
  Company,
  CreateCompanyInput,
  UpdateCompanyInput
} from 'types/billing';
import { REGIME_FISCALE_OPTIONS } from 'types/billing';
import OrkestraCloseButton from 'components/common/OrkestraCloseButton';

interface CompanyModalProps {
  show: boolean;
  onHide: () => void;
  company?: Company | null;
  onSuccess?: () => void;
}

const CompanyModal: React.FC<CompanyModalProps> = ({
  show,
  onHide,
  company,
  onSuccess
}) => {
  const isEditMode = !!company;
  const [createCompany, { isLoading: isCreating }] = useCreateCompanyMutation();
  const [updateCompany, { isLoading: isUpdating }] = useUpdateCompanyMutation();
  const isLoading = isCreating || isUpdating;

  // OpenAPI SDI Business Registry
  const [
    getBusinessRegistryConfig,
    { data: businessRegistryConfig, isLoading: isLoadingConfig }
  ] = useLazyGetBusinessRegistryConfigQuery();
  const [configureBusinessRegistry, { isLoading: isConfiguringRegistry }] =
    useConfigureBusinessRegistryMutation();

  const [error, setError] = useState<string>('');
  const [activeTab, setActiveTab] = useState<string>('general');

  // OpenAPI SDI state
  const [applySignature, setApplySignature] = useState(false);
  const [applyLegalStorage, setApplyLegalStorage] = useState(false);
  const [openApiSuccess, setOpenApiSuccess] = useState<string>('');
  const [openApiError, setOpenApiError] = useState<string>('');

  const initialFormData: CreateCompanyInput = {
    fiscalIdCountry: 'IT',
    fiscalIdCode: '',
    codiceFiscale: '',
    denomination: '',
    regimeFiscale: 'RF01',
    address: '',
    numeroCivico: '',
    city: '',
    province: '',
    postalCode: '',
    country: 'IT',
    // REA
    reaOffice: '',
    reaNumber: '',
    capitaleSociale: undefined,
    socioUnico: undefined,
    statoLiquidazione: 'LN',
    // Contacts
    email: '',
    pec: '',
    phone: '',
    // Bank
    iban: '',
    bic: '',
    abi: '',
    cab: '',
    beneficiario: '',
    istitutoFinanziario: '',
    notes: '',
    isProfessional: false
  };

  const [formData, setFormData] = useState<CreateCompanyInput>(initialFormData);

  // Populate form when editing
  useEffect(() => {
    if (company && show) {
      setFormData({
        fiscalIdCountry: company.fiscalIdCountry || 'IT',
        fiscalIdCode: company.fiscalIdCode || '',
        codiceFiscale: company.codiceFiscale || '',
        denomination: company.denomination || '',
        regimeFiscale: company.regimeFiscale || 'RF01',
        address: company.address || '',
        numeroCivico: company.numeroCivico || '',
        city: company.city || '',
        province: company.province || '',
        postalCode: company.postalCode || '',
        country: company.country || 'IT',
        // REA
        reaOffice: company.reaOffice || '',
        reaNumber: company.reaNumber || '',
        capitaleSociale: company.capitaleSociale,
        socioUnico: company.socioUnico,
        statoLiquidazione: company.statoLiquidazione || 'LN',
        // Contacts
        email: company.email || '',
        pec: company.pec || '',
        phone: company.phone || '',
        // Bank
        iban: company.iban || '',
        bic: company.bic || '',
        abi: company.abi || '',
        cab: company.cab || '',
        beneficiario: company.beneficiario || '',
        istitutoFinanziario: company.istitutoFinanziario || '',
        notes: company.notes || '',
        isProfessional: company.isProfessional || false
      });
    }
  }, [company, show]);

  // Load OpenAPI Business Registry config when editing
  useEffect(() => {
    if (isEditMode && company?.fiscalIdCode && show) {
      getBusinessRegistryConfig(company.fiscalIdCode);
    }
  }, [isEditMode, company?.fiscalIdCode, show, getBusinessRegistryConfig]);

  // Populate OpenAPI settings from config
  useEffect(() => {
    if (businessRegistryConfig) {
      setApplySignature(businessRegistryConfig.applySignature || false);
      setApplyLegalStorage(businessRegistryConfig.applyLegalStorage || false);
    }
  }, [businessRegistryConfig]);

  const handleChange = (
    e: React.ChangeEvent<
      HTMLInputElement | HTMLSelectElement | HTMLTextAreaElement
    >
  ) => {
    const { name, value, type } = e.target;
    const checked = (e.target as HTMLInputElement).checked;

    setFormData((prev: CreateCompanyInput) => ({
      ...prev,
      [name]: type === 'checkbox' ? checked : value
    }));
    setError('');
  };

  const handleNumberChange = (e: React.ChangeEvent<HTMLInputElement>) => {
    const { name, value } = e.target;
    setFormData((prev: CreateCompanyInput) => ({
      ...prev,
      [name]: value ? parseFloat(value) : undefined
    }));
    setError('');
  };

  // Register company on OpenAPI SDI
  const handleRegisterOpenAPI = async () => {
    if (!company?.fiscalIdCode) return;

    setOpenApiError('');
    setOpenApiSuccess('');

    const email = formData.email || formData.pec;
    if (!email || !email.includes('@')) {
      setOpenApiError('Email o PEC richiesta per la registrazione OpenAPI');
      return;
    }

    try {
      const result = await configureBusinessRegistry({
        fiscalId: company.fiscalIdCode,
        email,
        applySignature,
        applyLegalStorage
      }).unwrap();

      if (result.success) {
        setOpenApiSuccess(
          result.message || 'Registrazione su OpenAPI SDI completata'
        );
        // Refresh config
        getBusinessRegistryConfig(company.fiscalIdCode);
      } else {
        setOpenApiError(result.message || 'Errore durante la registrazione');
      }
    } catch (err: unknown) {
      const apiError = err as {
        data?: {
          detail?: string;
          title?: string;
          message?: string;
        };
        status?: number;
      };

      const errorMessage =
        apiError?.data?.detail ||
        apiError?.data?.title ||
        apiError?.data?.message ||
        (err instanceof Error ? err.message : null) ||
        'Errore durante la registrazione su OpenAPI SDI';

      setOpenApiError(errorMessage);
    }
  };

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    setError('');

    // Validation
    if (!formData.fiscalIdCode.trim()) {
      setError('La Partita IVA è obbligatoria');
      setActiveTab('general');
      return;
    }

    if (!formData.denomination?.trim()) {
      setError('La Ragione Sociale è obbligatoria');
      setActiveTab('general');
      return;
    }

    if (
      !formData.address.trim() ||
      !formData.city.trim() ||
      !formData.postalCode.trim()
    ) {
      setError('Indirizzo, Città e CAP sono obbligatori');
      setActiveTab('address');
      return;
    }

    try {
      if (isEditMode && company) {
        const updateData: UpdateCompanyInput = {
          denomination: formData.denomination,
          regimeFiscale: formData.regimeFiscale,
          address: formData.address,
          numeroCivico: formData.numeroCivico,
          city: formData.city,
          province: formData.province,
          postalCode: formData.postalCode,
          country: formData.country,
          // REA
          reaOffice: formData.reaOffice,
          reaNumber: formData.reaNumber,
          capitaleSociale: formData.capitaleSociale,
          socioUnico: formData.socioUnico,
          statoLiquidazione: formData.statoLiquidazione,
          // Contacts
          email: formData.email,
          pec: formData.pec,
          phone: formData.phone,
          // Bank
          iban: formData.iban,
          bic: formData.bic,
          abi: formData.abi,
          cab: formData.cab,
          beneficiario: formData.beneficiario,
          istitutoFinanziario: formData.istitutoFinanziario,
          notes: formData.notes,
          isProfessional: formData.isProfessional
        };
        await updateCompany({ id: company.id, data: updateData }).unwrap();
      } else {
        await createCompany(formData).unwrap();
      }

      handleClose();
      if (onSuccess) onSuccess();
    } catch (err: any) {
      setError(
        err?.data?.message ||
          `Errore durante ${isEditMode ? 'il salvataggio' : 'la creazione'} dell'azienda`
      );
    }
  };

  const handleClose = () => {
    setFormData(initialFormData);
    setError('');
    setActiveTab('general');
    // Reset OpenAPI state
    setApplySignature(false);
    setApplyLegalStorage(false);
    setOpenApiSuccess('');
    setOpenApiError('');
    onHide();
  };

  return (
    <Modal show={show} onHide={handleClose} centered size="lg">
      <Modal.Header>
        <Modal.Title>
          {isEditMode ? 'Modifica Azienda' : 'Nuova Azienda'}
        </Modal.Title>
        <OrkestraCloseButton onClick={handleClose} />
      </Modal.Header>
      <Form onSubmit={handleSubmit}>
        <Modal.Body>
          {error && (
            <Alert variant="danger" dismissible onClose={() => setError('')}>
              {error}
            </Alert>
          )}

          <Tab.Container
            activeKey={activeTab}
            onSelect={k => setActiveTab(k || 'general')}
          >
            <Nav variant="tabs" className="mb-3">
              <Nav.Item>
                <Nav.Link eventKey="general">Dati Generali</Nav.Link>
              </Nav.Item>
              <Nav.Item>
                <Nav.Link eventKey="address">Indirizzo</Nav.Link>
              </Nav.Item>
              <Nav.Item>
                <Nav.Link eventKey="rea">REA</Nav.Link>
              </Nav.Item>
              <Nav.Item>
                <Nav.Link eventKey="contact">Contatti</Nav.Link>
              </Nav.Item>
              <Nav.Item>
                <Nav.Link eventKey="bank">Banca</Nav.Link>
              </Nav.Item>
              <Nav.Item>
                <Nav.Link eventKey="openapi">OpenAPI SDI</Nav.Link>
              </Nav.Item>
            </Nav>

            <Tab.Content>
              {/* General Tab */}
              <Tab.Pane eventKey="general">
                <Form.Group className="mb-3">
                  <Form.Label>
                    Ragione Sociale <span className="text-danger">*</span>
                  </Form.Label>
                  <Form.Control
                    type="text"
                    name="denomination"
                    value={formData.denomination}
                    onChange={handleChange}
                    placeholder="es. Acme S.r.l."
                    required
                  />
                </Form.Group>

                <Row>
                  <Col md={2}>
                    <Form.Group className="mb-3">
                      <Form.Label>Paese</Form.Label>
                      <Form.Control
                        type="text"
                        name="fiscalIdCountry"
                        value={formData.fiscalIdCountry}
                        onChange={handleChange}
                        maxLength={2}
                        disabled={isEditMode}
                      />
                    </Form.Group>
                  </Col>
                  <Col md={5}>
                    <Form.Group className="mb-3">
                      <Form.Label>
                        Partita IVA <span className="text-danger">*</span>
                      </Form.Label>
                      <Form.Control
                        type="text"
                        name="fiscalIdCode"
                        value={formData.fiscalIdCode}
                        onChange={handleChange}
                        placeholder="es. 12345678901"
                        maxLength={11}
                        disabled={isEditMode}
                        required
                      />
                    </Form.Group>
                  </Col>
                  <Col md={5}>
                    <Form.Group className="mb-3">
                      <Form.Label>Codice Fiscale</Form.Label>
                      <Form.Control
                        type="text"
                        name="codiceFiscale"
                        value={formData.codiceFiscale}
                        onChange={handleChange}
                        placeholder="es. RSSMRA80A01H501Z"
                        maxLength={16}
                        disabled={isEditMode}
                      />
                      <Form.Text className="text-muted">
                        Solo se diverso dalla P.IVA
                      </Form.Text>
                    </Form.Group>
                  </Col>
                </Row>

                <Form.Group className="mb-3">
                  <Form.Label>
                    Regime Fiscale <span className="text-danger">*</span>
                  </Form.Label>
                  <Form.Select
                    name="regimeFiscale"
                    value={formData.regimeFiscale}
                    onChange={handleChange}
                    required
                  >
                    {REGIME_FISCALE_OPTIONS.map(option => (
                      <option key={option.value} value={option.value}>
                        {option.label}
                      </option>
                    ))}
                  </Form.Select>
                </Form.Group>

                {formData.regimeFiscale === 'RF19' && (
                  <Form.Group className="mb-3 mt-3">
                    <Form.Check
                      type="switch"
                      id="isProfessional"
                      name="isProfessional"
                      label="Professionista"
                      checked={formData.isProfessional || false}
                      onChange={handleChange}
                    />
                    <Form.Text className="text-muted">
                      Attiva per professionisti in regime forfettario:
                      disabilita ritenuta d'acconto e pre-compila la cassa
                      previdenziale in fattura
                    </Form.Text>
                  </Form.Group>
                )}
              </Tab.Pane>

              {/* Address Tab */}
              <Tab.Pane eventKey="address">
                <Row>
                  <Col md={9}>
                    <Form.Group className="mb-3">
                      <Form.Label>
                        Indirizzo <span className="text-danger">*</span>
                      </Form.Label>
                      <Form.Control
                        type="text"
                        name="address"
                        value={formData.address}
                        onChange={handleChange}
                        placeholder="es. Via Roma"
                        required
                      />
                    </Form.Group>
                  </Col>
                  <Col md={3}>
                    <Form.Group className="mb-3">
                      <Form.Label>N. Civico</Form.Label>
                      <Form.Control
                        type="text"
                        name="numeroCivico"
                        value={formData.numeroCivico}
                        onChange={handleChange}
                        placeholder="123"
                        maxLength={10}
                      />
                    </Form.Group>
                  </Col>
                </Row>

                <Row>
                  <Col md={6}>
                    <Form.Group className="mb-3">
                      <Form.Label>
                        Città <span className="text-danger">*</span>
                      </Form.Label>
                      <Form.Control
                        type="text"
                        name="city"
                        value={formData.city}
                        onChange={handleChange}
                        placeholder="es. Roma"
                        required
                      />
                    </Form.Group>
                  </Col>
                  <Col md={2}>
                    <Form.Group className="mb-3">
                      <Form.Label>Prov.</Form.Label>
                      <Form.Control
                        type="text"
                        name="province"
                        value={formData.province}
                        onChange={handleChange}
                        placeholder="RM"
                        maxLength={2}
                      />
                    </Form.Group>
                  </Col>
                  <Col md={2}>
                    <Form.Group className="mb-3">
                      <Form.Label>
                        CAP <span className="text-danger">*</span>
                      </Form.Label>
                      <Form.Control
                        type="text"
                        name="postalCode"
                        value={formData.postalCode}
                        onChange={handleChange}
                        placeholder="00100"
                        maxLength={5}
                        required
                      />
                    </Form.Group>
                  </Col>
                  <Col md={2}>
                    <Form.Group className="mb-3">
                      <Form.Label>Nazione</Form.Label>
                      <Form.Control
                        type="text"
                        name="country"
                        value={formData.country}
                        onChange={handleChange}
                        maxLength={2}
                      />
                    </Form.Group>
                  </Col>
                </Row>
              </Tab.Pane>

              {/* REA Tab */}
              <Tab.Pane eventKey="rea">
                <Row>
                  <Col md={3}>
                    <Form.Group className="mb-3">
                      <Form.Label>Ufficio REA</Form.Label>
                      <Form.Control
                        type="text"
                        name="reaOffice"
                        value={formData.reaOffice}
                        onChange={handleChange}
                        placeholder="es. RM"
                        maxLength={2}
                      />
                      <Form.Text className="text-muted">
                        Sigla provincia
                      </Form.Text>
                    </Form.Group>
                  </Col>
                  <Col md={5}>
                    <Form.Group className="mb-3">
                      <Form.Label>Numero REA</Form.Label>
                      <Form.Control
                        type="text"
                        name="reaNumber"
                        value={formData.reaNumber}
                        onChange={handleChange}
                        placeholder="es. 123456"
                        maxLength={20}
                      />
                    </Form.Group>
                  </Col>
                  <Col md={4}>
                    <Form.Group className="mb-3">
                      <Form.Label>Capitale Sociale</Form.Label>
                      <Form.Control
                        type="number"
                        name="capitaleSociale"
                        value={formData.capitaleSociale || ''}
                        onChange={handleNumberChange}
                        placeholder="es. 10000.00"
                        step="0.01"
                        min="0"
                      />
                    </Form.Group>
                  </Col>
                </Row>

                <Row>
                  <Col md={6}>
                    <Form.Group className="mb-3">
                      <Form.Label>Socio Unico</Form.Label>
                      <Form.Select
                        name="socioUnico"
                        value={formData.socioUnico || ''}
                        onChange={handleChange}
                      >
                        <option value="">Non specificato</option>
                        <option value="SU">SU - Socio Unico</option>
                        <option value="SM">SM - Più Soci</option>
                      </Form.Select>
                    </Form.Group>
                  </Col>
                  <Col md={6}>
                    <Form.Group className="mb-3">
                      <Form.Label>Stato Liquidazione</Form.Label>
                      <Form.Select
                        name="statoLiquidazione"
                        value={formData.statoLiquidazione || 'LN'}
                        onChange={handleChange}
                      >
                        <option value="LN">LN - Non in liquidazione</option>
                        <option value="LS">LS - In liquidazione</option>
                      </Form.Select>
                    </Form.Group>
                  </Col>
                </Row>

                <Alert variant="info" className="mt-3">
                  <small>
                    I dati REA (Repertorio Economico Amministrativo) sono
                    obbligatori per le società di capitali e cooperative
                    iscritte al Registro Imprese.
                  </small>
                </Alert>
              </Tab.Pane>

              {/* Contact Tab */}
              <Tab.Pane eventKey="contact">
                <Form.Group className="mb-3">
                  <Form.Label>Email</Form.Label>
                  <Form.Control
                    type="email"
                    name="email"
                    value={formData.email}
                    onChange={handleChange}
                    placeholder="es. info@azienda.it"
                  />
                </Form.Group>

                <Form.Group className="mb-3">
                  <Form.Label>PEC</Form.Label>
                  <Form.Control
                    type="email"
                    name="pec"
                    value={formData.pec}
                    onChange={handleChange}
                    placeholder="es. azienda@pec.it"
                  />
                  <Form.Text className="text-muted">
                    Posta Elettronica Certificata
                  </Form.Text>
                </Form.Group>

                <Form.Group className="mb-3">
                  <Form.Label>Telefono</Form.Label>
                  <Form.Control
                    type="tel"
                    name="phone"
                    value={formData.phone}
                    onChange={handleChange}
                    placeholder="es. +39 06 1234567"
                  />
                </Form.Group>

                <Form.Group className="mb-3">
                  <Form.Label>Note</Form.Label>
                  <Form.Control
                    as="textarea"
                    rows={3}
                    name="notes"
                    value={formData.notes}
                    onChange={handleChange}
                    placeholder="Note interne..."
                  />
                </Form.Group>
              </Tab.Pane>

              {/* Bank Tab */}
              <Tab.Pane eventKey="bank">
                <Form.Group className="mb-3">
                  <Form.Label>IBAN</Form.Label>
                  <Form.Control
                    type="text"
                    name="iban"
                    value={formData.iban}
                    onChange={handleChange}
                    placeholder="es. IT60X0542811101000000123456"
                    maxLength={34}
                  />
                </Form.Group>

                <Row>
                  <Col md={4}>
                    <Form.Group className="mb-3">
                      <Form.Label>BIC/SWIFT</Form.Label>
                      <Form.Control
                        type="text"
                        name="bic"
                        value={formData.bic}
                        onChange={handleChange}
                        placeholder="es. BLOPIT22"
                        maxLength={11}
                      />
                    </Form.Group>
                  </Col>
                  <Col md={4}>
                    <Form.Group className="mb-3">
                      <Form.Label>ABI</Form.Label>
                      <Form.Control
                        type="text"
                        name="abi"
                        value={formData.abi}
                        onChange={handleChange}
                        placeholder="es. 05428"
                        maxLength={5}
                      />
                    </Form.Group>
                  </Col>
                  <Col md={4}>
                    <Form.Group className="mb-3">
                      <Form.Label>CAB</Form.Label>
                      <Form.Control
                        type="text"
                        name="cab"
                        value={formData.cab}
                        onChange={handleChange}
                        placeholder="es. 11101"
                        maxLength={5}
                      />
                    </Form.Group>
                  </Col>
                </Row>

                <Form.Group className="mb-3">
                  <Form.Label>Beneficiario</Form.Label>
                  <Form.Control
                    type="text"
                    name="beneficiario"
                    value={formData.beneficiario}
                    onChange={handleChange}
                    placeholder="es. Acme S.r.l."
                  />
                  <Form.Text className="text-muted">
                    Intestatario del conto corrente
                  </Form.Text>
                </Form.Group>

                <Form.Group className="mb-3">
                  <Form.Label>Istituto Finanziario</Form.Label>
                  <Form.Control
                    type="text"
                    name="istitutoFinanziario"
                    value={formData.istitutoFinanziario}
                    onChange={handleChange}
                    placeholder="es. Banca XYZ S.p.A."
                  />
                </Form.Group>
              </Tab.Pane>

              {/* OpenAPI SDI Tab */}
              <Tab.Pane eventKey="openapi">
                {openApiError && (
                  <Alert
                    variant="danger"
                    dismissible
                    onClose={() => setOpenApiError('')}
                  >
                    {openApiError}
                  </Alert>
                )}
                {openApiSuccess && (
                  <Alert
                    variant="success"
                    dismissible
                    onClose={() => setOpenApiSuccess('')}
                  >
                    {openApiSuccess}
                  </Alert>
                )}

                {isEditMode ? (
                  <>
                    <Alert variant="info" className="mb-3">
                      <strong>
                        Registrazione Business Registry OpenAPI SDI
                      </strong>
                      <p className="mb-0 mt-1">
                        Prima di poter inviare fatture elettroniche tramite
                        OpenAPI SDI, è necessario registrare l'anagrafica
                        aziendale.
                      </p>
                    </Alert>

                    {businessRegistryConfig?.active && (
                      <Alert variant="success" className="mb-3">
                        <strong>Stato: Registrato</strong>
                        <ul className="mb-0 mt-2">
                          <li>Email: {businessRegistryConfig.email}</li>
                          <li>
                            Firma digitale:{' '}
                            {businessRegistryConfig.applySignature
                              ? 'Attiva'
                              : 'Non attiva'}
                          </li>
                          <li>
                            Conservazione:{' '}
                            {businessRegistryConfig.applyLegalStorage
                              ? 'Attiva'
                              : 'Non attiva'}
                          </li>
                        </ul>
                      </Alert>
                    )}

                    <Row className="mb-3">
                      <Col md={6}>
                        <Form.Check
                          type="switch"
                          id="applySignature"
                          label="Applica firma digitale"
                          checked={applySignature}
                          onChange={e => setApplySignature(e.target.checked)}
                        />
                        <Form.Text className="text-muted d-block mt-1">
                          Firma digitalmente le fatture prima dell'invio
                        </Form.Text>
                      </Col>
                      <Col md={6}>
                        <Form.Check
                          type="switch"
                          id="applyLegalStorage"
                          label="Conservazione sostitutiva"
                          checked={applyLegalStorage}
                          onChange={e => setApplyLegalStorage(e.target.checked)}
                        />
                        <Form.Text className="text-muted d-block mt-1">
                          Archivia le fatture in conservazione a norma
                        </Form.Text>
                      </Col>
                    </Row>

                    <Alert variant="secondary" className="mb-3">
                      <small>
                        L'email per le notifiche SDI sarà:{' '}
                        <strong>
                          {formData.email ||
                            formData.pec ||
                            '(non configurata)'}
                        </strong>
                        <br />
                        Per modificarla, vai alla tab "Contatti".
                      </small>
                    </Alert>

                    <div className="d-flex justify-content-end">
                      <Button
                        variant="primary"
                        onClick={handleRegisterOpenAPI}
                        disabled={isConfiguringRegistry || isLoadingConfig}
                      >
                        {isConfiguringRegistry ? (
                          <>
                            <Spinner size="sm" className="me-2" />
                            Registrazione...
                          </>
                        ) : businessRegistryConfig?.active ? (
                          'Aggiorna configurazione OpenAPI SDI'
                        ) : (
                          'Registra su OpenAPI SDI'
                        )}
                      </Button>
                    </div>
                  </>
                ) : (
                  <Alert variant="warning">
                    <strong>Azienda non ancora salvata</strong>
                    <p className="mb-0 mt-1">
                      Salva prima l'azienda per poterla registrare su OpenAPI
                      SDI.
                    </p>
                  </Alert>
                )}
              </Tab.Pane>
            </Tab.Content>
          </Tab.Container>
        </Modal.Body>
        <Modal.Footer>
          <Button
            variant="secondary"
            onClick={handleClose}
            disabled={isLoading}
          >
            Annulla
          </Button>
          <Button variant="primary" type="submit" disabled={isLoading}>
            {isLoading
              ? isEditMode
                ? 'Salvataggio...'
                : 'Creazione...'
              : isEditMode
                ? 'Salva Modifiche'
                : 'Crea Azienda'}
          </Button>
        </Modal.Footer>
      </Form>
    </Modal>
  );
};

export default CompanyModal;
