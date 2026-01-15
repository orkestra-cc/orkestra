import React, { useState } from 'react';
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome';
import { Button, Card, Collapse, Row, Col, Badge, Form } from 'react-bootstrap';
import { CraneResponse, useUpdateCraneMutation } from 'store/api/craneApi';
import { FaEdit, FaSave, FaTimes, FaChevronDown, FaChevronUp } from 'react-icons/fa';
import { GiCrane } from 'react-icons/gi';

interface CraneProfileInfoProps {
  crane: CraneResponse;
}

const CraneProfileInfo: React.FC<CraneProfileInfoProps> = ({ crane }) => {
  const [collapsed, setCollapsed] = useState(false);
  const [isEditing, setIsEditing] = useState(false);
  const [updateCrane, { isLoading }] = useUpdateCraneMutation();

  const [formData, setFormData] = useState({
    nome: crane.nome,
    tipo: crane.tipo,
    matricola: crane.matricola,
    verificareSuMezzo: crane.verificareSuMezzo || '',
    note: crane.note || '',
    scadenzaVerifica: crane.scadenzaVerifica ?
      new Date(crane.scadenzaVerifica).toISOString().split('T')[0] : ''
  });

  // Helper function to format date with time
  const formatDateTime = (dateString: string) => {
    return new Date(dateString).toLocaleString('en-GB', {
      year: 'numeric',
      month: 'long',
      day: 'numeric',
      hour: '2-digit',
      minute: '2-digit'
    });
  };

  // Helper function to format date only
  const formatDate = (dateString?: string) => {
    if (!dateString) return 'Not specified';
    return new Date(dateString).toLocaleDateString('en-GB', {
      year: 'numeric',
      month: 'long',
      day: 'numeric'
    });
  };

  const craneTypes = [
    'Autogrù',
    'Gru a torre',
    'Gru mobile',
    'Gru fissa',
    'Gru semovente',
    'Gru su camion',
    'Gru cingolata',
    'Altro'
  ];

  const handleEdit = () => {
    setIsEditing(true);
    setCollapsed(false);
  };

  const handleCancel = () => {
    setIsEditing(false);
    // Reset form data to original values
    setFormData({
      nome: crane.nome,
      tipo: crane.tipo,
      matricola: crane.matricola,
      verificareSuMezzo: crane.verificareSuMezzo || '',
      note: crane.note || '',
      scadenzaVerifica: crane.scadenzaVerifica ?
        new Date(crane.scadenzaVerifica).toISOString().split('T')[0] : ''
    });
  };

  const handleSave = async () => {
    try {
      const dataToSubmit: any = {
        nome: formData.nome,
        tipo: formData.tipo,
        matricola: formData.matricola
      };

      // Add optional fields if they have values
      if (formData.verificareSuMezzo) dataToSubmit.verificareSuMezzo = formData.verificareSuMezzo.toUpperCase();
      if (formData.note) dataToSubmit.note = formData.note;
      if (formData.scadenzaVerifica) {
        dataToSubmit.scadenzaVerifica = new Date(formData.scadenzaVerifica).toISOString();
      }

      await updateCrane({
        id: crane.id,
        data: dataToSubmit
      }).unwrap();

      setIsEditing(false);
    } catch (error) {
      console.error('Failed to update crane:', error);
    }
  };

  const handleChange = (e: React.ChangeEvent<HTMLInputElement | HTMLSelectElement | HTMLTextAreaElement>) => {
    const { name, value } = e.target;
    setFormData(prev => ({
      ...prev,
      [name]: value
    }));
  };

  return (
    <Card className="mb-3">
      <Card.Header className="bg-body-tertiary d-flex justify-content-between align-items-center">
        <h5 className="mb-0">Crane Information</h5>
        <div>
          {!isEditing ? (
            <>
              <Button
                variant="falcon-default"
                size="sm"
                className="me-2"
                onClick={handleEdit}
              >
                <FaEdit className="me-1" /> Edit
              </Button>
              <Button
                variant="link"
                size="sm"
                onClick={() => setCollapsed(!collapsed)}
              >
                {collapsed ? <FaChevronDown /> : <FaChevronUp />}
              </Button>
            </>
          ) : (
            <>
              <Button
                variant="success"
                size="sm"
                className="me-2"
                onClick={handleSave}
                disabled={isLoading}
              >
                <FaSave className="me-1" /> Save
              </Button>
              <Button
                variant="secondary"
                size="sm"
                onClick={handleCancel}
                disabled={isLoading}
              >
                <FaTimes className="me-1" /> Cancel
              </Button>
            </>
          )}
        </div>
      </Card.Header>

      <Collapse in={!collapsed}>
        <Card.Body className="text-1000">
          {!isEditing ? (
            <>
              {/* View Mode */}
              <Row className="g-3">
                <Col md={6}>
                  <div className="mb-3">
                    <h6 className="fw-semibold text-600">Crane Name</h6>
                    <p className="mb-1">
                      <GiCrane className="me-2 text-warning" />
                      {crane.nome}
                    </p>
                  </div>
                </Col>
                <Col md={6}>
                  <div className="mb-3">
                    <h6 className="fw-semibold text-600">Type</h6>
                    <Badge bg="warning" className="text-dark">
                      {crane.tipo}
                    </Badge>
                  </div>
                </Col>
                <Col md={6}>
                  <div className="mb-3">
                    <h6 className="fw-semibold text-600">Serial Number</h6>
                    <p className="mb-1">
                      <FontAwesomeIcon icon="id-card" className="me-2" />
                      {crane.matricola}
                    </p>
                  </div>
                </Col>
                <Col md={6}>
                  <div className="mb-3">
                    <h6 className="fw-semibold text-600">Associated Vehicle</h6>
                    {crane.verificareSuMezzo ? (
                      <Badge bg="info" className="fs-10">
                        <FontAwesomeIcon icon="truck" className="me-1" />
                        {crane.verificareSuMezzo}
                      </Badge>
                    ) : (
                      <span className="text-muted">Not associated</span>
                    )}
                  </div>
                </Col>
                <Col md={6}>
                  <div className="mb-3">
                    <h6 className="fw-semibold text-600">Verification Expiry</h6>
                    <p className="mb-1">
                      <FontAwesomeIcon icon="calendar-alt" className="me-2" />
                      {formatDate(crane.scadenzaVerifica)}
                    </p>
                  </div>
                </Col>
                <Col md={6}>
                  <div className="mb-3">
                    <h6 className="fw-semibold text-600">Status</h6>
                    <Badge bg={crane.isActive ? 'success' : 'secondary'}>
                      {crane.isActive ? 'Active' : 'Inactive'}
                    </Badge>
                  </div>
                </Col>
                <Col xs={12}>
                  <div className="mb-3">
                    <h6 className="fw-semibold text-600">Notes</h6>
                    <p className="mb-1">
                      {crane.note || <span className="text-muted">No notes</span>}
                    </p>
                  </div>
                </Col>
                <Col xs={12}>
                  <hr />
                  <Row>
                    <Col sm={6}>
                      <small className="text-muted">
                        <FontAwesomeIcon icon="clock" className="me-1" />
                        Created On: {formatDateTime(crane.createdAt)}
                      </small>
                    </Col>
                    <Col sm={6} className="text-sm-end">
                      <small className="text-muted">
                        <FontAwesomeIcon icon="edit" className="me-1" />
                        Updated On: {formatDateTime(crane.updatedAt)}
                      </small>
                    </Col>
                  </Row>
                </Col>
              </Row>
            </>
          ) : (
            <>
              {/* Edit Mode */}
              <Row className="g-3">
                <Col md={6}>
                  <Form.Group>
                    <Form.Label>Crane Name</Form.Label>
                    <Form.Control
                      type="text"
                      name="nome"
                      value={formData.nome}
                      onChange={handleChange}
                      placeholder="Crane name"
                      required
                    />
                  </Form.Group>
                </Col>
                <Col md={6}>
                  <Form.Group>
                    <Form.Label>Type</Form.Label>
                    <Form.Select
                      name="tipo"
                      value={formData.tipo}
                      onChange={handleChange}
                      required
                    >
                      {craneTypes.map(type => (
                        <option key={type} value={type}>{type}</option>
                      ))}
                    </Form.Select>
                  </Form.Group>
                </Col>
                <Col md={6}>
                  <Form.Group>
                    <Form.Label>Serial Number</Form.Label>
                    <Form.Control
                      type="text"
                      name="matricola"
                      value={formData.matricola}
                      onChange={handleChange}
                      placeholder="Crane serial number"
                      required
                    />
                  </Form.Group>
                </Col>
                <Col md={6}>
                  <Form.Group>
                    <Form.Label>Associated Vehicle (License Plate)</Form.Label>
                    <Form.Control
                      type="text"
                      name="verificareSuMezzo"
                      value={formData.verificareSuMezzo}
                      onChange={handleChange}
                      placeholder="E.g. AA123BB"
                    />
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
                    />
                  </Form.Group>
                </Col>
                <Col xs={12}>
                  <Form.Group>
                    <Form.Label>Notes</Form.Label>
                    <Form.Control
                      as="textarea"
                      rows={3}
                      name="note"
                      value={formData.note}
                      onChange={handleChange}
                      placeholder="Any notes about the crane..."
                    />
                  </Form.Group>
                </Col>
              </Row>
            </>
          )}
        </Card.Body>
      </Collapse>
    </Card>
  );
};

export default CraneProfileInfo;