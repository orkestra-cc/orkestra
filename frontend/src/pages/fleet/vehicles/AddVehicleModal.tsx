import React, { useState } from 'react';
import { Modal, Button, Form, Row, Col, Alert } from 'react-bootstrap';
import FalconCloseButton from 'components/common/FalconCloseButton';
import { useCreateVehicleMutation } from 'store/api/vehicleApi';
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome';

interface AddVehicleModalProps {
  show: boolean;
  onHide: () => void;
}

const AddVehicleModal: React.FC<AddVehicleModalProps> = ({ show, onHide }) => {
  const [createVehicle, { isLoading, error }] = useCreateVehicleMutation();
  const [formData, setFormData] = useState({
    nome: '',
    targa: '',
    tipo: 'motrice',
    luogo: '',
    note: '',
    scadenzaRevisione: '',
    revisioneProgrammata: '',
    insuranceExpiry: '',
    carTaxExpiry: ''
  });

  const [validationErrors, setValidationErrors] = useState<
    Record<string, string>
  >({});

  const handleChange = (
    e: React.ChangeEvent<
      HTMLInputElement | HTMLSelectElement | HTMLTextAreaElement
    >
  ) => {
    const { name, value } = e.target;
    setFormData(prev => ({
      ...prev,
      [name]: value
    }));
    // Clear validation error for this field
    if (validationErrors[name]) {
      setValidationErrors(prev => {
        const newErrors = { ...prev };
        delete newErrors[name];
        return newErrors;
      });
    }
  };

  const validateForm = () => {
    const errors: Record<string, string> = {};

    if (!formData.nome.trim()) {
      errors.nome = 'Vehicle name is required';
    }

    if (!formData.targa.trim()) {
      errors.targa = 'License plate is required';
    } else if (!/^[A-Z0-9]+$/i.test(formData.targa.replace(/\s/g, ''))) {
      errors.targa = 'License plate must contain only letters and numbers';
    }

    if (!formData.tipo) {
      errors.tipo = 'Vehicle type is required';
    }

    // Validate dates if provided
    if (formData.scadenzaRevisione) {
      const date = new Date(formData.scadenzaRevisione);
      if (isNaN(date.getTime())) {
        errors.scadenzaRevisione = 'Invalid date';
      }
    }

    if (formData.revisioneProgrammata) {
      const date = new Date(formData.revisioneProgrammata);
      if (isNaN(date.getTime())) {
        errors.revisioneProgrammata = 'Invalid date';
      }
    }

    if (formData.insuranceExpiry) {
      const date = new Date(formData.insuranceExpiry);
      if (isNaN(date.getTime())) {
        errors.insuranceExpiry = 'Invalid date';
      }
    }

    if (formData.carTaxExpiry) {
      const date = new Date(formData.carTaxExpiry);
      if (isNaN(date.getTime())) {
        errors.carTaxExpiry = 'Invalid date';
      }
    }

    setValidationErrors(errors);
    return Object.keys(errors).length === 0;
  };

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();

    if (!validateForm()) {
      return;
    }

    try {
      // Prepare data for submission
      const dataToSubmit: any = {
        nome: formData.nome.trim(),
        targa: formData.targa.trim().toUpperCase(),
        tipo: formData.tipo
      };

      // Add optional fields if they have values
      if (formData.luogo.trim()) {
        dataToSubmit.luogo = formData.luogo.trim();
      }
      if (formData.note.trim()) {
        dataToSubmit.note = formData.note.trim();
      }
      if (formData.scadenzaRevisione) {
        dataToSubmit.scadenzaRevisione = new Date(
          formData.scadenzaRevisione
        ).toISOString();
      }
      if (formData.revisioneProgrammata) {
        dataToSubmit.revisioneProgrammata = new Date(
          formData.revisioneProgrammata
        ).toISOString();
      }
      if (formData.insuranceExpiry) {
        dataToSubmit.insuranceExpiry = new Date(
          formData.insuranceExpiry
        ).toISOString();
      }
      if (formData.carTaxExpiry) {
        dataToSubmit.carTaxExpiry = new Date(
          formData.carTaxExpiry
        ).toISOString();
      }

      await createVehicle(dataToSubmit).unwrap();

      // Reset form and close modal on success
      setFormData({
        nome: '',
        targa: '',
        tipo: 'motrice',
        luogo: '',
        note: '',
        scadenzaRevisione: '',
        revisioneProgrammata: '',
        insuranceExpiry: '',
        carTaxExpiry: ''
      });
      setValidationErrors({});
      onHide();
    } catch (err) {
      console.error('Failed to create vehicle:', err);
    }
  };

  const handleClose = () => {
    // Reset form when closing
    setFormData({
      nome: '',
      targa: '',
      tipo: 'motrice',
      luogo: '',
      note: '',
      scadenzaRevisione: '',
      revisioneProgrammata: '',
      insuranceExpiry: '',
      carTaxExpiry: ''
    });
    setValidationErrors({});
    onHide();
  };

  return (
    <Modal show={show} onHide={handleClose} size="lg" centered>
      <Form onSubmit={handleSubmit}>
        <Modal.Header>
          <Modal.Title>
            <FontAwesomeIcon icon="plus" className="me-2" />
            Add New Vehicle
          </Modal.Title>
          <FalconCloseButton onClick={handleClose} />
        </Modal.Header>
        <Modal.Body>
          {error && (
            <Alert variant="danger" className="mb-3">
              <FontAwesomeIcon icon="exclamation-triangle" className="me-2" />
              An error occurred while creating the vehicle. Please try again.
            </Alert>
          )}

          <Row className="g-3">
            <Col md={6}>
              <Form.Group>
                <Form.Label>
                  Vehicle Name <span className="text-danger">*</span>
                </Form.Label>
                <Form.Control
                  type="text"
                  name="nome"
                  value={formData.nome}
                  onChange={handleChange}
                  isInvalid={!!validationErrors.nome}
                  placeholder="e.g. Iveco Daily 001"
                  required
                />
                <Form.Control.Feedback type="invalid">
                  {validationErrors.nome}
                </Form.Control.Feedback>
              </Form.Group>
            </Col>

            <Col md={6}>
              <Form.Group>
                <Form.Label>
                  License Plate <span className="text-danger">*</span>
                </Form.Label>
                <Form.Control
                  type="text"
                  name="targa"
                  value={formData.targa}
                  onChange={handleChange}
                  isInvalid={!!validationErrors.targa}
                  placeholder="e.g. AA123BB"
                  required
                />
                <Form.Control.Feedback type="invalid">
                  {validationErrors.targa}
                </Form.Control.Feedback>
              </Form.Group>
            </Col>

            <Col md={6}>
              <Form.Group>
                <Form.Label>
                  Tipo <span className="text-danger">*</span>
                </Form.Label>
                <Form.Select
                  name="tipo"
                  value={formData.tipo}
                  onChange={handleChange}
                  isInvalid={!!validationErrors.tipo}
                  required
                >
                  <option value="motrice">Truck</option>
                  <option value="rimorchio">Trailer</option>
                  <option value="semi-rimorchio">Semi-trailer</option>
                  <option value="trattore">Tractor</option>
                  <option value="semovente">Self-propelled</option>
                </Form.Select>
                <Form.Control.Feedback type="invalid">
                  {validationErrors.tipo}
                </Form.Control.Feedback>
              </Form.Group>
            </Col>

            <Col md={6}>
              <Form.Group>
                <Form.Label>Current Location</Form.Label>
                <Form.Control
                  type="text"
                  name="luogo"
                  value={formData.luogo}
                  onChange={handleChange}
                  placeholder="e.g. Main Depot"
                />
              </Form.Group>
            </Col>

            <Col md={6}>
              <Form.Group>
                <Form.Label>Inspection Expiry</Form.Label>
                <Form.Control
                  type="date"
                  name="scadenzaRevisione"
                  value={formData.scadenzaRevisione}
                  onChange={handleChange}
                  isInvalid={!!validationErrors.scadenzaRevisione}
                />
                <Form.Control.Feedback type="invalid">
                  {validationErrors.scadenzaRevisione}
                </Form.Control.Feedback>
              </Form.Group>
            </Col>

            <Col md={6}>
              <Form.Group>
                <Form.Label>Scheduled Inspection</Form.Label>
                <Form.Control
                  type="date"
                  name="revisioneProgrammata"
                  value={formData.revisioneProgrammata}
                  onChange={handleChange}
                  isInvalid={!!validationErrors.revisioneProgrammata}
                />
                <Form.Control.Feedback type="invalid">
                  {validationErrors.revisioneProgrammata}
                </Form.Control.Feedback>
              </Form.Group>
            </Col>

            <Col md={6}>
              <Form.Group>
                <Form.Label>Insurance Expiry</Form.Label>
                <Form.Control
                  type="date"
                  name="insuranceExpiry"
                  value={formData.insuranceExpiry}
                  onChange={handleChange}
                  isInvalid={!!validationErrors.insuranceExpiry}
                />
                <Form.Control.Feedback type="invalid">
                  {validationErrors.insuranceExpiry}
                </Form.Control.Feedback>
              </Form.Group>
            </Col>

            <Col md={6}>
              <Form.Group>
                <Form.Label>Vehicle Tax Expiry</Form.Label>
                <Form.Control
                  type="date"
                  name="carTaxExpiry"
                  value={formData.carTaxExpiry}
                  onChange={handleChange}
                  isInvalid={!!validationErrors.carTaxExpiry}
                />
                <Form.Control.Feedback type="invalid">
                  {validationErrors.carTaxExpiry}
                </Form.Control.Feedback>
              </Form.Group>
            </Col>

            <Col xs={12}>
              <Form.Group>
                <Form.Label>Note</Form.Label>
                <Form.Control
                  as="textarea"
                  rows={3}
                  name="note"
                  value={formData.note}
                  onChange={handleChange}
                  placeholder="Enter any notes about the vehicle..."
                />
              </Form.Group>
            </Col>
          </Row>
        </Modal.Body>
        <Modal.Footer>
          <Button
            variant="secondary"
            onClick={handleClose}
            disabled={isLoading}
          >
            Cancel
          </Button>
          <Button variant="primary" type="submit" disabled={isLoading}>
            {isLoading ? (
              <>
                <FontAwesomeIcon icon="spinner" spin className="me-2" />
                Creating...
              </>
            ) : (
              <>
                <FontAwesomeIcon icon="plus" className="me-2" />
                Create Vehicle
              </>
            )}
          </Button>
        </Modal.Footer>
      </Form>
    </Modal>
  );
};

export default AddVehicleModal;
