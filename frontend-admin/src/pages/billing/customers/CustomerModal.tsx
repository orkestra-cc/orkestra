import React, { useState, useEffect } from 'react';
import { Modal, Button, Form, Alert, Tab, Nav, Row, Col } from 'react-bootstrap';
import {
  useCreateCustomerMutation,
  useUpdateCustomerMutation,
} from 'store/api/billingApi';
import type { Customer, CreateCustomerInput, UpdateCustomerInput } from 'types/billing';
import FalconCloseButton from 'components/common/FalconCloseButton';

interface CustomerModalProps {
  show: boolean;
  onHide: () => void;
  customer?: Customer | null; // If provided, edit mode
  onSuccess?: () => void;
}

const CustomerModal: React.FC<CustomerModalProps> = ({
  show,
  onHide,
  customer,
  onSuccess
}) => {
  const isEditMode = !!customer;
  const [createCustomer, { isLoading: isCreating }] = useCreateCustomerMutation();
  const [updateCustomer, { isLoading: isUpdating }] = useUpdateCustomerMutation();
  const isLoading = isCreating || isUpdating;

  const [error, setError] = useState<string>('');
  const [activeTab, setActiveTab] = useState<string>('general');

  const initialFormData: CreateCustomerInput = {
    fiscalIdCountry: 'IT',
    fiscalIdCode: '',
    codiceFiscale: '',
    isCompany: true,
    denomination: '',
    name: '',
    surname: '',
    address: '',
    numeroCivico: '',
    city: '',
    province: '',
    postalCode: '',
    country: 'IT',
    email: '',
    pec: '',
    phone: '',
    codiceDestinatario: '',
    pecDestinatario: '',
    isPA: false,
    codiceUfficio: '',
    notes: '',
  };

  const [formData, setFormData] = useState<CreateCustomerInput>(initialFormData);

  // Populate form when editing
  useEffect(() => {
    if (customer && show) {
      setFormData({
        fiscalIdCountry: customer.fiscalIdCountry || 'IT',
        fiscalIdCode: customer.fiscalIdCode || '',
        codiceFiscale: customer.codiceFiscale || '',
        isCompany: customer.isCompany,
        denomination: customer.denomination || '',
        name: customer.name || '',
        surname: customer.surname || '',
        address: customer.address || '',
        numeroCivico: customer.numeroCivico || '',
        city: customer.city || '',
        province: customer.province || '',
        postalCode: customer.postalCode || '',
        country: customer.country || 'IT',
        email: customer.email || '',
        pec: customer.pec || '',
        phone: customer.phone || '',
        codiceDestinatario: customer.codiceDestinatario || '',
        pecDestinatario: customer.pecDestinatario || '',
        isPA: customer.isPA || false,
        codiceUfficio: customer.codiceUfficio || '',
        notes: customer.notes || '',
      });
    }
  }, [customer, show]);

  const handleChange = (
    e: React.ChangeEvent<HTMLInputElement | HTMLSelectElement | HTMLTextAreaElement>
  ) => {
    const { name, value, type } = e.target;
    const checked = (e.target as HTMLInputElement).checked;

    setFormData((prev: CreateCustomerInput) => ({
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

    // SDI delivery validation
    if (!formData.codiceDestinatario && !formData.pecDestinatario) {
      setError('Inserire il Codice Destinatario SDI oppure la PEC per la ricezione');
      setActiveTab('sdi');
      return;
    }

    try {
      if (isEditMode && customer) {
        const updateData: UpdateCustomerInput = {
          denomination: formData.denomination,
          name: formData.name,
          surname: formData.surname,
          address: formData.address,
          numeroCivico: formData.numeroCivico,
          city: formData.city,
          province: formData.province,
          postalCode: formData.postalCode,
          email: formData.email,
          pec: formData.pec,
          phone: formData.phone,
          codiceDestinatario: formData.codiceDestinatario,
          pecDestinatario: formData.pecDestinatario,
          notes: formData.notes,
        };
        await updateCustomer({ id: customer.id, data: updateData }).unwrap();
      } else {
        await createCustomer(formData).unwrap();
      }

      handleClose();
      if (onSuccess) onSuccess();
    } catch (err: any) {
      setError(err?.data?.message || `Errore durante il ${isEditMode ? 'salvataggio' : 'creazione'} del cliente`);
    }
  };

  const handleClose = () => {
    setFormData(initialFormData);
    setError('');
    setActiveTab('general');
    onHide();
  };

  return (
    <Modal show={show} onHide={handleClose} centered size="lg">
      <Modal.Header>
        <Modal.Title>
          {isEditMode ? 'Modifica Cliente' : 'Nuovo Cliente'}
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
                <Nav.Link eventKey="sdi">SDI</Nav.Link>
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
                      placeholder="es. Acme S.r.l."
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

              {/* SDI Tab */}
              <Tab.Pane eventKey="sdi">
                <Form.Group className="mb-3">
                  <Form.Check
                    type="switch"
                    id="isPA"
                    name="isPA"
                    label="Pubblica Amministrazione"
                    checked={formData.isPA}
                    onChange={handleChange}
                    disabled={isEditMode}
                  />
                </Form.Group>

                <Form.Group className="mb-3">
                  <Form.Label>
                    Codice Destinatario SDI {!formData.pecDestinatario && <span className="text-danger">*</span>}
                  </Form.Label>
                  <Form.Control
                    type="text"
                    name="codiceDestinatario"
                    value={formData.codiceDestinatario}
                    onChange={handleChange}
                    placeholder={formData.isPA ? 'es. UFXXX1' : 'es. A1B2C3D'}
                    maxLength={formData.isPA ? 6 : 7}
                  />
                  <Form.Text className="text-muted">
                    {formData.isPA
                      ? '6 caratteri per Pubbliche Amministrazioni'
                      : '7 caratteri per aziende private (es. 0000000 se non conosciuto)'}
                  </Form.Text>
                </Form.Group>

                <Form.Group className="mb-3">
                  <Form.Label>
                    PEC Destinatario {!formData.codiceDestinatario && <span className="text-danger">*</span>}
                  </Form.Label>
                  <Form.Control
                    type="email"
                    name="pecDestinatario"
                    value={formData.pecDestinatario}
                    onChange={handleChange}
                    placeholder="es. fatture@pec.azienda.it"
                  />
                  <Form.Text className="text-muted">
                    Alternativa al Codice Destinatario per la ricezione fatture
                  </Form.Text>
                </Form.Group>

                {formData.isPA && (
                  <Form.Group className="mb-3">
                    <Form.Label>Codice Ufficio</Form.Label>
                    <Form.Control
                      type="text"
                      name="codiceUfficio"
                      value={formData.codiceUfficio}
                      onChange={handleChange}
                      placeholder="es. UFXXX1"
                      maxLength={6}
                    />
                    <Form.Text className="text-muted">
                      Codice Univoco Ufficio per fatture PA
                    </Form.Text>
                  </Form.Group>
                )}
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
              : (isEditMode ? 'Salva Modifiche' : 'Crea Cliente')}
          </Button>
        </Modal.Footer>
      </Form>
    </Modal>
  );
};

export default CustomerModal;
