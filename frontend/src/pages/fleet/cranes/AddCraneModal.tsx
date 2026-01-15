import React, { useState } from 'react';
import { Modal, Button, Form, Row, Col, Alert } from 'react-bootstrap';
import FalconCloseButton from 'components/common/FalconCloseButton';
import { useCreateCraneMutation } from 'store/api/craneApi';
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome';

interface AddCraneModalProps {
  show: boolean;
  onHide: () => void;
}

const AddCraneModal: React.FC<AddCraneModalProps> = ({ show, onHide }) => {
  const [createCrane, { isLoading, error }] = useCreateCraneMutation();
  const [formData, setFormData] = useState({
    nome: '',
    tipo: '',
    matricola: '',
    verificareSuMezzo: '',
    scadenzaVerifica: '',
    note: ''
  });

  const [validationErrors, setValidationErrors] = useState<Record<string, string>>({});

  const handleChange = (e: React.ChangeEvent<HTMLInputElement | HTMLSelectElement | HTMLTextAreaElement>) => {
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
      errors.nome = 'Crane name is required';
    }

    if (!formData.tipo.trim()) {
      errors.tipo = 'Crane type is required';
    }

    if (!formData.matricola.trim()) {
      errors.matricola = 'Serial number is required';
    }

    // Validate date if provided
    if (formData.scadenzaVerifica) {
      const date = new Date(formData.scadenzaVerifica);
      if (isNaN(date.getTime())) {
        errors.scadenzaVerifica = 'Invalid date';
      }
    }

    // Validate mezzo targa format if provided
    if (formData.verificareSuMezzo && !/^[A-Z0-9]+$/i.test(formData.verificareSuMezzo.replace(/\s/g, ''))) {
      errors.verificareSuMezzo = 'Vehicle plate must contain only letters and numbers';
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
        tipo: formData.tipo.trim(),
        matricola: formData.matricola.trim()
      };

      // Add optional fields if they have values
      if (formData.verificareSuMezzo.trim()) {
        dataToSubmit.verificareSuMezzo = formData.verificareSuMezzo.trim().toUpperCase();
      }
      if (formData.scadenzaVerifica) {
        dataToSubmit.scadenzaVerifica = new Date(formData.scadenzaVerifica).toISOString();
      }
      if (formData.note.trim()) {
        dataToSubmit.note = formData.note.trim();
      }

      await createCrane(dataToSubmit).unwrap();

      // Reset form and close modal on success
      setFormData({
        nome: '',
        tipo: '',
        matricola: '',
        verificareSuMezzo: '',
        scadenzaVerifica: '',
        note: ''
      });
      setValidationErrors({});
      onHide();
    } catch (err) {
      console.error('Failed to create crane:', err);
    }
  };

  const handleClose = () => {
    // Reset form when closing
    setFormData({
      nome: '',
      tipo: '',
      matricola: '',
      verificareSuMezzo: '',
      scadenzaVerifica: '',
      note: ''
    });
    setValidationErrors({});
    onHide();
  };

  const craneTypes = [
    'Mobile Crane',
    'Tower Crane',
    'Crawler Crane',
    'Fixed Crane',
    'Self-propelled Crane',
    'Truck Mounted Crane',
    'All-terrain Crane',
    'Other'
  ];

  return (
    <Modal show={show} onHide={handleClose} size="lg" centered>
      <Form onSubmit={handleSubmit}>
        <Modal.Header>
          <Modal.Title>
            <FontAwesomeIcon icon="plus" className="me-2" />
            Add New Crane
          </Modal.Title>
          <FalconCloseButton onClick={handleClose} />
        </Modal.Header>
        <Modal.Body>
          {error && (
            <Alert variant="danger" className="mb-3">
              <FontAwesomeIcon icon="exclamation-triangle" className="me-2" />
              An error occurred while creating the crane. Please try again.
            </Alert>
          )}

          <Row className="g-3">
            <Col md={6}>
              <Form.Group>
                <Form.Label>
                  Crane Name <span className="text-danger">*</span>
                </Form.Label>
                <Form.Control
                  type="text"
                  name="nome"
                  value={formData.nome}
                  onChange={handleChange}
                  isInvalid={!!validationErrors.nome}
                  placeholder="e.g. Mobile Crane 01"
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
                  Tipo <span className="text-danger">*</span>
                </Form.Label>
                <Form.Select
                  name="tipo"
                  value={formData.tipo}
                  onChange={handleChange}
                  isInvalid={!!validationErrors.tipo}
                  required
                >
                  <option value="">Select type...</option>
                  {craneTypes.map(type => (
                    <option key={type} value={type}>{type}</option>
                  ))}
                </Form.Select>
                <Form.Control.Feedback type="invalid">
                  {validationErrors.tipo}
                </Form.Control.Feedback>
              </Form.Group>
            </Col>

            <Col md={6}>
              <Form.Group>
                <Form.Label>
                  Serial Number <span className="text-danger">*</span>
                </Form.Label>
                <Form.Control
                  type="text"
                  name="matricola"
                  value={formData.matricola}
                  onChange={handleChange}
                  isInvalid={!!validationErrors.matricola}
                  placeholder="Enter serial number"
                  required
                />
                <Form.Control.Feedback type="invalid">
                  {validationErrors.matricola}
                </Form.Control.Feedback>
              </Form.Group>
            </Col>

            <Col md={6}>
              <Form.Group>
                <Form.Label>Associated Vehicle Plate</Form.Label>
                <Form.Control
                  type="text"
                  name="verificareSuMezzo"
                  value={formData.verificareSuMezzo}
                  onChange={handleChange}
                  isInvalid={!!validationErrors.verificareSuMezzo}
                  placeholder="e.g. AA123BB"
                />
                <Form.Control.Feedback type="invalid">
                  {validationErrors.verificareSuMezzo}
                </Form.Control.Feedback>
                <Form.Text className="text-muted">
                  Enter the license plate of the vehicle the crane is mounted on
                </Form.Text>
              </Form.Group>
            </Col>

            <Col md={6}>
              <Form.Group>
                <Form.Label>Verification Expiry</Form.Label>
                <Form.Control
                  type="date"
                  name="scadenzaVerifica"
                  value={formData.scadenzaVerifica}
                  onChange={handleChange}
                  isInvalid={!!validationErrors.scadenzaVerifica}
                />
                <Form.Control.Feedback type="invalid">
                  {validationErrors.scadenzaVerifica}
                </Form.Control.Feedback>
                <Form.Text className="text-muted">
                  Periodic verification expiry date
                </Form.Text>
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
                  placeholder="Enter any notes about the crane..."
                />
              </Form.Group>
            </Col>
          </Row>
        </Modal.Body>
        <Modal.Footer>
          <Button variant="secondary" onClick={handleClose} disabled={isLoading}>
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
                Create Crane
              </>
            )}
          </Button>
        </Modal.Footer>
      </Form>
    </Modal>
  );
};

export default AddCraneModal;