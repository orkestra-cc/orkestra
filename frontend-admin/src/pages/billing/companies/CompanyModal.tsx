import React, { useState, useEffect } from 'react';
import { useTranslation } from 'react-i18next';
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
  const { t } = useTranslation();
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
      setOpenApiError(t('billing.companyModal.toasts.emailRequiredOpenApi'));
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
          result.message || t('billing.companyModal.toasts.registerSuccess')
        );
        // Refresh config
        getBusinessRegistryConfig(company.fiscalIdCode);
      } else {
        setOpenApiError(
          result.message ||
            t('billing.companyModal.toasts.registerErrorGeneric')
        );
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
        t('billing.companyModal.toasts.registerError');

      setOpenApiError(errorMessage);
    }
  };

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    setError('');

    // Validation
    if (!formData.fiscalIdCode.trim()) {
      setError(t('billing.companyModal.validation.vatRequired'));
      setActiveTab('general');
      return;
    }

    if (!formData.denomination?.trim()) {
      setError(t('billing.companyModal.validation.denominationRequired'));
      setActiveTab('general');
      return;
    }

    if (
      !formData.address.trim() ||
      !formData.city.trim() ||
      !formData.postalCode.trim()
    ) {
      setError(t('billing.companyModal.validation.addressRequired'));
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
          (isEditMode
            ? t('billing.companyModal.toasts.saveErrorEdit')
            : t('billing.companyModal.toasts.saveErrorCreate'))
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
          {isEditMode
            ? t('billing.companyModal.title.edit')
            : t('billing.companyModal.title.create')}
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
                <Nav.Link eventKey="general">
                  {t('billing.companyModal.tabs.general')}
                </Nav.Link>
              </Nav.Item>
              <Nav.Item>
                <Nav.Link eventKey="address">
                  {t('billing.companyModal.tabs.address')}
                </Nav.Link>
              </Nav.Item>
              <Nav.Item>
                <Nav.Link eventKey="rea">
                  {t('billing.companyModal.tabs.rea')}
                </Nav.Link>
              </Nav.Item>
              <Nav.Item>
                <Nav.Link eventKey="contact">
                  {t('billing.companyModal.tabs.contact')}
                </Nav.Link>
              </Nav.Item>
              <Nav.Item>
                <Nav.Link eventKey="bank">
                  {t('billing.companyModal.tabs.bank')}
                </Nav.Link>
              </Nav.Item>
              <Nav.Item>
                <Nav.Link eventKey="openapi">
                  {t('billing.companyModal.tabs.openapi')}
                </Nav.Link>
              </Nav.Item>
            </Nav>

            <Tab.Content>
              {/* General Tab */}
              <Tab.Pane eventKey="general">
                <Form.Group className="mb-3">
                  <Form.Label>
                    {t('billing.companyModal.general.denominationLabel')}{' '}
                    <span className="text-danger">*</span>
                  </Form.Label>
                  <Form.Control
                    type="text"
                    name="denomination"
                    value={formData.denomination}
                    onChange={handleChange}
                    placeholder={t(
                      'billing.companyModal.general.denominationPlaceholder'
                    )}
                    required
                  />
                </Form.Group>

                <Row>
                  <Col md={2}>
                    <Form.Group className="mb-3">
                      <Form.Label>
                        {t('billing.companyModal.general.countryLabel')}
                      </Form.Label>
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
                        {t('billing.companyModal.general.vatLabel')}{' '}
                        <span className="text-danger">*</span>
                      </Form.Label>
                      <Form.Control
                        type="text"
                        name="fiscalIdCode"
                        value={formData.fiscalIdCode}
                        onChange={handleChange}
                        placeholder={t(
                          'billing.companyModal.general.vatPlaceholder'
                        )}
                        maxLength={11}
                        disabled={isEditMode}
                        required
                      />
                    </Form.Group>
                  </Col>
                  <Col md={5}>
                    <Form.Group className="mb-3">
                      <Form.Label>
                        {t('billing.companyModal.general.codiceFiscaleLabel')}
                      </Form.Label>
                      <Form.Control
                        type="text"
                        name="codiceFiscale"
                        value={formData.codiceFiscale}
                        onChange={handleChange}
                        placeholder={t(
                          'billing.companyModal.general.codiceFiscalePlaceholder'
                        )}
                        maxLength={16}
                        disabled={isEditMode}
                      />
                      <Form.Text className="text-muted">
                        {t('billing.companyModal.general.codiceFiscaleHelp')}
                      </Form.Text>
                    </Form.Group>
                  </Col>
                </Row>

                <Form.Group className="mb-3">
                  <Form.Label>
                    {t('billing.companyModal.general.regimeFiscaleLabel')}{' '}
                    <span className="text-danger">*</span>
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
                      label={t(
                        'billing.companyModal.general.professionalLabel'
                      )}
                      checked={formData.isProfessional || false}
                      onChange={handleChange}
                    />
                    <Form.Text className="text-muted">
                      {t('billing.companyModal.general.professionalHelp')}
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
                        {t('billing.companyModal.address.addressLabel')}{' '}
                        <span className="text-danger">*</span>
                      </Form.Label>
                      <Form.Control
                        type="text"
                        name="address"
                        value={formData.address}
                        onChange={handleChange}
                        placeholder={t(
                          'billing.companyModal.address.addressPlaceholder'
                        )}
                        required
                      />
                    </Form.Group>
                  </Col>
                  <Col md={3}>
                    <Form.Group className="mb-3">
                      <Form.Label>
                        {t('billing.companyModal.address.numeroCivicoLabel')}
                      </Form.Label>
                      <Form.Control
                        type="text"
                        name="numeroCivico"
                        value={formData.numeroCivico}
                        onChange={handleChange}
                        placeholder={t(
                          'billing.companyModal.address.numeroCivicoPlaceholder'
                        )}
                        maxLength={10}
                      />
                    </Form.Group>
                  </Col>
                </Row>

                <Row>
                  <Col md={6}>
                    <Form.Group className="mb-3">
                      <Form.Label>
                        {t('billing.companyModal.address.cityLabel')}{' '}
                        <span className="text-danger">*</span>
                      </Form.Label>
                      <Form.Control
                        type="text"
                        name="city"
                        value={formData.city}
                        onChange={handleChange}
                        placeholder={t(
                          'billing.companyModal.address.cityPlaceholder'
                        )}
                        required
                      />
                    </Form.Group>
                  </Col>
                  <Col md={2}>
                    <Form.Group className="mb-3">
                      <Form.Label>
                        {t('billing.companyModal.address.provinceLabel')}
                      </Form.Label>
                      <Form.Control
                        type="text"
                        name="province"
                        value={formData.province}
                        onChange={handleChange}
                        placeholder={t(
                          'billing.companyModal.address.provincePlaceholder'
                        )}
                        maxLength={2}
                      />
                    </Form.Group>
                  </Col>
                  <Col md={2}>
                    <Form.Group className="mb-3">
                      <Form.Label>
                        {t('billing.companyModal.address.postalCodeLabel')}{' '}
                        <span className="text-danger">*</span>
                      </Form.Label>
                      <Form.Control
                        type="text"
                        name="postalCode"
                        value={formData.postalCode}
                        onChange={handleChange}
                        placeholder={t(
                          'billing.companyModal.address.postalCodePlaceholder'
                        )}
                        maxLength={5}
                        required
                      />
                    </Form.Group>
                  </Col>
                  <Col md={2}>
                    <Form.Group className="mb-3">
                      <Form.Label>
                        {t('billing.companyModal.address.countryLabel')}
                      </Form.Label>
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
                      <Form.Label>
                        {t('billing.companyModal.rea.officeLabel')}
                      </Form.Label>
                      <Form.Control
                        type="text"
                        name="reaOffice"
                        value={formData.reaOffice}
                        onChange={handleChange}
                        placeholder={t(
                          'billing.companyModal.rea.officePlaceholder'
                        )}
                        maxLength={2}
                      />
                      <Form.Text className="text-muted">
                        {t('billing.companyModal.rea.officeHelp')}
                      </Form.Text>
                    </Form.Group>
                  </Col>
                  <Col md={5}>
                    <Form.Group className="mb-3">
                      <Form.Label>
                        {t('billing.companyModal.rea.numberLabel')}
                      </Form.Label>
                      <Form.Control
                        type="text"
                        name="reaNumber"
                        value={formData.reaNumber}
                        onChange={handleChange}
                        placeholder={t(
                          'billing.companyModal.rea.numberPlaceholder'
                        )}
                        maxLength={20}
                      />
                    </Form.Group>
                  </Col>
                  <Col md={4}>
                    <Form.Group className="mb-3">
                      <Form.Label>
                        {t('billing.companyModal.rea.capitaleSocialeLabel')}
                      </Form.Label>
                      <Form.Control
                        type="number"
                        name="capitaleSociale"
                        value={formData.capitaleSociale || ''}
                        onChange={handleNumberChange}
                        placeholder={t(
                          'billing.companyModal.rea.capitaleSocialePlaceholder'
                        )}
                        step="0.01"
                        min="0"
                      />
                    </Form.Group>
                  </Col>
                </Row>

                <Row>
                  <Col md={6}>
                    <Form.Group className="mb-3">
                      <Form.Label>
                        {t('billing.companyModal.rea.socioUnicoLabel')}
                      </Form.Label>
                      <Form.Select
                        name="socioUnico"
                        value={formData.socioUnico || ''}
                        onChange={handleChange}
                      >
                        <option value="">
                          {t('billing.companyModal.rea.socioUnicoNotSpecified')}
                        </option>
                        <option value="SU">
                          {t('billing.companyModal.rea.socioUnicoSU')}
                        </option>
                        <option value="SM">
                          {t('billing.companyModal.rea.socioUnicoSM')}
                        </option>
                      </Form.Select>
                    </Form.Group>
                  </Col>
                  <Col md={6}>
                    <Form.Group className="mb-3">
                      <Form.Label>
                        {t('billing.companyModal.rea.statoLiquidazioneLabel')}
                      </Form.Label>
                      <Form.Select
                        name="statoLiquidazione"
                        value={formData.statoLiquidazione || 'LN'}
                        onChange={handleChange}
                      >
                        <option value="LN">
                          {t('billing.companyModal.rea.statoLiquidazioneLN')}
                        </option>
                        <option value="LS">
                          {t('billing.companyModal.rea.statoLiquidazioneLS')}
                        </option>
                      </Form.Select>
                    </Form.Group>
                  </Col>
                </Row>

                <Alert variant="info" className="mt-3">
                  <small>{t('billing.companyModal.rea.infoAlert')}</small>
                </Alert>
              </Tab.Pane>

              {/* Contact Tab */}
              <Tab.Pane eventKey="contact">
                <Form.Group className="mb-3">
                  <Form.Label>
                    {t('billing.companyModal.contact.emailLabel')}
                  </Form.Label>
                  <Form.Control
                    type="email"
                    name="email"
                    value={formData.email}
                    onChange={handleChange}
                    placeholder={t(
                      'billing.companyModal.contact.emailPlaceholder'
                    )}
                  />
                </Form.Group>

                <Form.Group className="mb-3">
                  <Form.Label>
                    {t('billing.companyModal.contact.pecLabel')}
                  </Form.Label>
                  <Form.Control
                    type="email"
                    name="pec"
                    value={formData.pec}
                    onChange={handleChange}
                    placeholder={t(
                      'billing.companyModal.contact.pecPlaceholder'
                    )}
                  />
                  <Form.Text className="text-muted">
                    {t('billing.companyModal.contact.pecHelp')}
                  </Form.Text>
                </Form.Group>

                <Form.Group className="mb-3">
                  <Form.Label>
                    {t('billing.companyModal.contact.phoneLabel')}
                  </Form.Label>
                  <Form.Control
                    type="tel"
                    name="phone"
                    value={formData.phone}
                    onChange={handleChange}
                    placeholder={t(
                      'billing.companyModal.contact.phonePlaceholder'
                    )}
                  />
                </Form.Group>

                <Form.Group className="mb-3">
                  <Form.Label>
                    {t('billing.companyModal.contact.notesLabel')}
                  </Form.Label>
                  <Form.Control
                    as="textarea"
                    rows={3}
                    name="notes"
                    value={formData.notes}
                    onChange={handleChange}
                    placeholder={t(
                      'billing.companyModal.contact.notesPlaceholder'
                    )}
                  />
                </Form.Group>
              </Tab.Pane>

              {/* Bank Tab */}
              <Tab.Pane eventKey="bank">
                <Form.Group className="mb-3">
                  <Form.Label>
                    {t('billing.companyModal.bank.ibanLabel')}
                  </Form.Label>
                  <Form.Control
                    type="text"
                    name="iban"
                    value={formData.iban}
                    onChange={handleChange}
                    placeholder={t('billing.companyModal.bank.ibanPlaceholder')}
                    maxLength={34}
                  />
                </Form.Group>

                <Row>
                  <Col md={4}>
                    <Form.Group className="mb-3">
                      <Form.Label>
                        {t('billing.companyModal.bank.bicLabel')}
                      </Form.Label>
                      <Form.Control
                        type="text"
                        name="bic"
                        value={formData.bic}
                        onChange={handleChange}
                        placeholder={t(
                          'billing.companyModal.bank.bicPlaceholder'
                        )}
                        maxLength={11}
                      />
                    </Form.Group>
                  </Col>
                  <Col md={4}>
                    <Form.Group className="mb-3">
                      <Form.Label>
                        {t('billing.companyModal.bank.abiLabel')}
                      </Form.Label>
                      <Form.Control
                        type="text"
                        name="abi"
                        value={formData.abi}
                        onChange={handleChange}
                        placeholder={t(
                          'billing.companyModal.bank.abiPlaceholder'
                        )}
                        maxLength={5}
                      />
                    </Form.Group>
                  </Col>
                  <Col md={4}>
                    <Form.Group className="mb-3">
                      <Form.Label>
                        {t('billing.companyModal.bank.cabLabel')}
                      </Form.Label>
                      <Form.Control
                        type="text"
                        name="cab"
                        value={formData.cab}
                        onChange={handleChange}
                        placeholder={t(
                          'billing.companyModal.bank.cabPlaceholder'
                        )}
                        maxLength={5}
                      />
                    </Form.Group>
                  </Col>
                </Row>

                <Form.Group className="mb-3">
                  <Form.Label>
                    {t('billing.companyModal.bank.beneficiarioLabel')}
                  </Form.Label>
                  <Form.Control
                    type="text"
                    name="beneficiario"
                    value={formData.beneficiario}
                    onChange={handleChange}
                    placeholder={t(
                      'billing.companyModal.bank.beneficiarioPlaceholder'
                    )}
                  />
                  <Form.Text className="text-muted">
                    {t('billing.companyModal.bank.beneficiarioHelp')}
                  </Form.Text>
                </Form.Group>

                <Form.Group className="mb-3">
                  <Form.Label>
                    {t('billing.companyModal.bank.istitutoLabel')}
                  </Form.Label>
                  <Form.Control
                    type="text"
                    name="istitutoFinanziario"
                    value={formData.istitutoFinanziario}
                    onChange={handleChange}
                    placeholder={t(
                      'billing.companyModal.bank.istitutoPlaceholder'
                    )}
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
                        {t('billing.companyModal.openapi.infoTitle')}
                      </strong>
                      <p className="mb-0 mt-1">
                        {t('billing.companyModal.openapi.infoBody')}
                      </p>
                    </Alert>

                    {businessRegistryConfig?.active && (
                      <Alert variant="success" className="mb-3">
                        <strong>
                          {t(
                            'billing.companyModal.openapi.statusRegisteredTitle'
                          )}
                        </strong>
                        <ul className="mb-0 mt-2">
                          <li>
                            {t('billing.companyModal.openapi.statusEmail')}{' '}
                            {businessRegistryConfig.email}
                          </li>
                          <li>
                            {t('billing.companyModal.openapi.statusSignature')}{' '}
                            {businessRegistryConfig.applySignature
                              ? t('billing.companyModal.openapi.statusActive')
                              : t(
                                  'billing.companyModal.openapi.statusInactive'
                                )}
                          </li>
                          <li>
                            {t('billing.companyModal.openapi.statusStorage')}{' '}
                            {businessRegistryConfig.applyLegalStorage
                              ? t('billing.companyModal.openapi.statusActive')
                              : t(
                                  'billing.companyModal.openapi.statusInactive'
                                )}
                          </li>
                        </ul>
                      </Alert>
                    )}

                    <Row className="mb-3">
                      <Col md={6}>
                        <Form.Check
                          type="switch"
                          id="applySignature"
                          label={t(
                            'billing.companyModal.openapi.signatureSwitch'
                          )}
                          checked={applySignature}
                          onChange={e => setApplySignature(e.target.checked)}
                        />
                        <Form.Text className="text-muted d-block mt-1">
                          {t('billing.companyModal.openapi.signatureHelp')}
                        </Form.Text>
                      </Col>
                      <Col md={6}>
                        <Form.Check
                          type="switch"
                          id="applyLegalStorage"
                          label={t(
                            'billing.companyModal.openapi.storageSwitch'
                          )}
                          checked={applyLegalStorage}
                          onChange={e => setApplyLegalStorage(e.target.checked)}
                        />
                        <Form.Text className="text-muted d-block mt-1">
                          {t('billing.companyModal.openapi.storageHelp')}
                        </Form.Text>
                      </Col>
                    </Row>

                    <Alert variant="secondary" className="mb-3">
                      <small>
                        {t('billing.companyModal.openapi.emailNoteIntro')}{' '}
                        <strong>
                          {formData.email ||
                            formData.pec ||
                            t(
                              'billing.companyModal.openapi.emailNotConfigured'
                            )}
                        </strong>
                        <br />
                        {t('billing.companyModal.openapi.emailNoteOutro')}
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
                            {t('billing.companyModal.openapi.registering')}
                          </>
                        ) : businessRegistryConfig?.active ? (
                          t('billing.companyModal.openapi.updateButton')
                        ) : (
                          t('billing.companyModal.openapi.registerButton')
                        )}
                      </Button>
                    </div>
                  </>
                ) : (
                  <Alert variant="warning">
                    <strong>
                      {t('billing.companyModal.openapi.notSavedTitle')}
                    </strong>
                    <p className="mb-0 mt-1">
                      {t('billing.companyModal.openapi.notSavedBody')}
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
            {t('billing.companyModal.actions.cancel')}
          </Button>
          <Button variant="primary" type="submit" disabled={isLoading}>
            {isLoading
              ? isEditMode
                ? t('billing.companyModal.actions.saving')
                : t('billing.companyModal.actions.creating')
              : isEditMode
                ? t('billing.companyModal.actions.save')
                : t('billing.companyModal.actions.create')}
          </Button>
        </Modal.Footer>
      </Form>
    </Modal>
  );
};

export default CompanyModal;
