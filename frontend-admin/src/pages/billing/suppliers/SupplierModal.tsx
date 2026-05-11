import React, { useState, useEffect } from 'react';
import { Modal, Button, Form, Alert, Tab, Nav, Row, Col } from 'react-bootstrap';
import {
  useCreateSupplierMutation,
  useUpdateSupplierMutation,
} from 'store/api/billingApi';
import type { Supplier, CreateSupplierInput, UpdateSupplierInput, RegimeFiscale } from 'types/billing';
import { REGIME_FISCALE_LABELS } from 'types/billing';
import FalconCloseButton from 'components/common/FalconCloseButton';

interface SupplierModalProps {
  show: boolean;
  onHide: () => void;
  supplier?: Supplier | null; // If provided, edit mode
  onSuccess?: () => void;
}

const SupplierModal: React.FC<SupplierModalProps> = ({
  show,
  onHide,
  supplier,
  onSuccess
}) => {
  const isEditMode = !!supplier;
  const [createSupplier, { isLoading: isCreating }] = useCreateSupplierMutation();
  const [updateSupplier, { isLoading: isUpdating }] = useUpdateSupplierMutation();
  const isLoading = isCreating || isUpdating;

  const [error, setError] = useState<string>('');
  const [activeTab, setActiveTab] = useState<string>('general');

  const initialFormData: CreateSupplierInput = {
    fiscalIdCountry: 'IT',
    fiscalIdCode: '',
    codiceFiscale: '',
    isCompany: true,
    denomination: '',
    name: '',
    surname: '',
    regimeFiscale: 'RF01',
    address: '',
    numeroCivico: '',
    city: '',
    province: '',
    postalCode: '',
    country: 'IT',
    email: '',
    pec: '',
    phone: '',
    iban: '',
    bic: '',
    notes: '',
  };

  const [formData, setFormData] = useState<CreateSupplierInput>(initialFormData);

  // Populate form when editing
  useEffect(() => {
    if (supplier && show) {
      setFormData({
        fiscalIdCountry: supplier.fiscalIdCountry || 'IT',
        fiscalIdCode: supplier.fiscalIdCode || '',
        codiceFiscale: supplier.codiceFiscale || '',
        isCompany: supplier.isCompany,
        denomination: supplier.denomination || '',
        name: supplier.name || '',
        surname: supplier.surname || '',
        regimeFiscale: supplier.regimeFiscale || 'RF01',
        address: supplier.address || '',
        numeroCivico: supplier.numeroCivico || '',
        city: supplier.city || '',
        province: supplier.province || '',
        postalCode: supplier.postalCode || '',
        country: supplier.country || 'IT',
        email: supplier.email || '',
        pec: supplier.pec || '',
        phone: supplier.phone || '',
        iban: supplier.iban || '',
        bic: supplier.bic || '',
        notes: supplier.notes || '',
      });
    }
  }, [supplier, show]);

  const handleChange = (
    e: React.ChangeEvent<HTMLInputElement | HTMLSelectElement | HTMLTextAreaElement>
  ) => {
    const { name, value, type } = e.target;
    const checked = (e.target as HTMLInputElement).checked;

    setFormData((prev: CreateSupplierInput) => ({
      ...prev,
      [name]: type === 'checkbox' ? checked : value
    }));
    setError('');
  };

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    setError('');

    // Validation
    if (!formData.fiscalIdCode.trim()) {
      setError('La Partita IVA / Codice Fiscale è obbligatorio');
      setActiveTab('general');
      return;
    }

    if (formData.isCompany && !formData.denomination?.trim()) {
      setError('La Ragione Sociale è obbligatoria per le aziende');
      setActiveTab('general');
      return;
    }

    if (!formData.isCompany && (!formData.name?.trim() || !formData.surname?.trim())) {
      setError('Nome e Cognome sono obbligatori per le persone fisiche');
      setActiveTab('general');
      return;
    }

    if (!formData.address.trim() || !formData.city.trim() || !formData.postalCode.trim()) {
      setError('Indirizzo, Città e CAP sono obbligatori');
      setActiveTab('address');
      return;
    }

    try {
      if (isEditMode && supplier) {
        const updateData: UpdateSupplierInput = {
          denomination: formData.denomination,
          name: formData.name,
          surname: formData.surname,
          regimeFiscale: formData.regimeFiscale,
          address: formData.address,
          numeroCivico: formData.numeroCivico,
          city: formData.city,
          province: formData.province,
          postalCode: formData.postalCode,
          email: formData.email,
          pec: formData.pec,
          phone: formData.phone,
          iban: formData.iban,
          bic: formData.bic,
          notes: formData.notes,
        };
        await updateSupplier({ id: supplier.id, data: updateData }).unwrap();
      } else {
        await createSupplier(formData).unwrap();
      }

      handleClose();
      if (onSuccess) onSuccess();
    } catch (err: any) {
      setError(err?.data?.message || `Errore durante il ${isEditMode ? 'salvataggio' : 'creazione'} del fornitore`);
    }
  };

  const handleClose = () => {
    setFormData(initialFormData);
    setError('');
    setActiveTab('general');
    onHide();
  };

  // Regime fiscale options
  const regimeFiscaleOptions: { value: RegimeFiscale; label: string }[] = (
    Object.entries(REGIME_FISCALE_LABELS) as [RegimeFiscale, string][]
  ).map(([value, label]) => ({
    value,
    label: `${value} - ${label}`
  }));

  return (
    <Modal show={show} onHide={handleClose} centered size="lg">
      <Modal.Header>
        <Modal.Title>
          {isEditMode ? 'Modifica Fornitore' : 'Nuovo Fornitore'}
        </Modal.Title>
        <FalconCloseButton onClick={handleClose} />
      </Modal.Header>
      <Form onSubmit={handleSubmit}>
        <Modal.Body>
          {error && (
            <Alert variant="danger" dismissible onClose={() => setError('')}>
              {error}
            </Alert>
          )}

          <Tab.Container activeKey={activeTab} onSelect={(k) => setActiveTab(k || 'general')}>
            <Nav variant="tabs" className="mb-3">
              <Nav.Item>
                <Nav.Link eventKey="general">Dati Generali</Nav.Link>
              </Nav.Item>
              <Nav.Item>
                <Nav.Link eventKey="address">Indirizzo</Nav.Link>
              </Nav.Item>
              <Nav.Item>
                <Nav.Link eventKey="contact">Contatti</Nav.Link>
              </Nav.Item>
              <Nav.Item>
                <Nav.Link eventKey="bank">Dati Bancari</Nav.Link>
              </Nav.Item>
            </Nav>

            <Tab.Content>
              {/* General Tab */}
              <Tab.Pane eventKey="general">
                <Form.Group className="mb-3">
                  <Form.Check
                    type="switch"
                    id="isCompany"
                    name="isCompany"
                    label="Azienda"
                    checked={formData.isCompany}
                    onChange={handleChange}
                    disabled={isEditMode}
                  />
                </Form.Group>

                {formData.isCompany ? (
                  <Form.Group className="mb-3">
                    <Form.Label>
                      Ragione Sociale <span className="text-danger">*</span>
                    </Form.Label>
                    <Form.Control
                      type="text"
                      name="denomination"
                      value={formData.denomination}
                      onChange={handleChange}
                      placeholder="es. Fornitore S.r.l."
                      required
                    />
                  </Form.Group>
                ) : (
                  <Row>
                    <Col md={6}>
                      <Form.Group className="mb-3">
                        <Form.Label>
                          Nome <span className="text-danger">*</span>
                        </Form.Label>
                        <Form.Control
                          type="text"
                          name="name"
                          value={formData.name}
                          onChange={handleChange}
                          placeholder="es. Mario"
                          required
                        />
                      </Form.Group>
                    </Col>
                    <Col md={6}>
                      <Form.Group className="mb-3">
                        <Form.Label>
                          Cognome <span className="text-danger">*</span>
                        </Form.Label>
                        <Form.Control
                          type="text"
                          name="surname"
                          value={formData.surname}
                          onChange={handleChange}
                          placeholder="es. Rossi"
                          required
                        />
                      </Form.Group>
                    </Col>
                  </Row>
                )}

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
                  <Form.Label>Regime Fiscale</Form.Label>
                  <Form.Select
                    name="regimeFiscale"
                    value={formData.regimeFiscale}
                    onChange={handleChange}
                  >
                    {regimeFiscaleOptions.map(option => (
                      <option key={option.value} value={option.value}>
                        {option.label}
                      </option>
                    ))}
                  </Form.Select>
                </Form.Group>
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

              {/* Contact Tab */}
              <Tab.Pane eventKey="contact">
                <Form.Group className="mb-3">
                  <Form.Label>Email</Form.Label>
                  <Form.Control
                    type="email"
                    name="email"
                    value={formData.email}
                    onChange={handleChange}
                    placeholder="es. info@fornitore.it"
                  />
                </Form.Group>

                <Form.Group className="mb-3">
                  <Form.Label>PEC</Form.Label>
                  <Form.Control
                    type="email"
                    name="pec"
                    value={formData.pec}
                    onChange={handleChange}
                    placeholder="es. fornitore@pec.it"
                  />
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
                  <Form.Text className="text-muted">
                    Coordinate bancarie per pagamenti al fornitore
                  </Form.Text>
                </Form.Group>

                <Form.Group className="mb-3">
                  <Form.Label>BIC/SWIFT</Form.Label>
                  <Form.Control
                    type="text"
                    name="bic"
                    value={formData.bic}
                    onChange={handleChange}
                    placeholder="es. BCITITMM"
                    maxLength={11}
                  />
                </Form.Group>
              </Tab.Pane>
            </Tab.Content>
          </Tab.Container>
        </Modal.Body>
        <Modal.Footer>
          <Button variant="secondary" onClick={handleClose} disabled={isLoading}>
            Annulla
          </Button>
          <Button variant="primary" type="submit" disabled={isLoading}>
            {isLoading
              ? (isEditMode ? 'Salvataggio...' : 'Creazione...')
              : (isEditMode ? 'Salva Modifiche' : 'Crea Fornitore')}
          </Button>
        </Modal.Footer>
      </Form>
    </Modal>
  );
};

export default SupplierModal;
