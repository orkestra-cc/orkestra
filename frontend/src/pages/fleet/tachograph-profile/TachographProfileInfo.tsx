import React, { useState } from 'react';
import { Card, Form, Button, Row, Col, Badge } from 'react-bootstrap';
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome';
import { TachographResponse, useUpdateTachographMutation } from 'store/api/tachographApi';

interface TachographProfileInfoProps {
  tachograph: TachographResponse;
}

const TachographProfileInfo: React.FC<TachographProfileInfoProps> = ({ tachograph }) => {
  const [isEditing, setIsEditing] = useState(false);
  const [updateTachograph, { isLoading: isUpdating }] = useUpdateTachographMutation();
  const [formData, setFormData] = useState({
    nome: tachograph.nome,
    targa: tachograph.targa,
    luogo: tachograph.luogo || '',
    scadenzaRevisione: tachograph.scadenzaRevisione ?
      new Date(tachograph.scadenzaRevisione).toISOString().split('T')[0] : '',
    revisioneProgrammata: tachograph.revisioneProgrammata ?
      new Date(tachograph.revisioneProgrammata).toISOString().split('T')[0] : '',
    note: tachograph.note || ''
  });

  const handleChange = (e: React.ChangeEvent<HTMLInputElement | HTMLTextAreaElement>) => {
    const { name, value } = e.target;
    setFormData(prev => ({
      ...prev,
      [name]: value
    }));
  };

  const handleSubmit = async () => {
    try {
      const dataToSubmit: any = {
        nome: formData.nome.trim(),
        targa: formData.targa.trim().toUpperCase()
      };

      if (formData.luogo.trim()) {
        dataToSubmit.luogo = formData.luogo.trim();
      }
      if (formData.note.trim()) {
        dataToSubmit.note = formData.note.trim();
      }
      if (formData.scadenzaRevisione) {
        dataToSubmit.scadenzaRevisione = new Date(formData.scadenzaRevisione).toISOString();
      }
      if (formData.revisioneProgrammata) {
        dataToSubmit.revisioneProgrammata = new Date(formData.revisioneProgrammata).toISOString();
      }

      await updateTachograph({
        id: tachograph.id,
        data: dataToSubmit
      }).unwrap();

      setIsEditing(false);
    } catch (error) {
      console.error('Failed to update tachograph:', error);
    }
  };

  const handleCancel = () => {
    setFormData({
      nome: tachograph.nome,
      targa: tachograph.targa,
      luogo: tachograph.luogo || '',
      scadenzaRevisione: tachograph.scadenzaRevisione ?
        new Date(tachograph.scadenzaRevisione).toISOString().split('T')[0] : '',
      revisioneProgrammata: tachograph.revisioneProgrammata ?
        new Date(tachograph.revisioneProgrammata).toISOString().split('T')[0] : '',
      note: tachograph.note || ''
    });
    setIsEditing(false);
  };

  const formatDate = (dateString?: string) => {
    if (!dateString) return '-';
    const date = new Date(dateString);
    return date.toLocaleDateString('en-GB', {
      year: 'numeric',
      month: 'long',
      day: 'numeric'
    });
  };

  const isRevisionExpiring = (date?: string) => {
    if (!date) return false;
    const revisionDate = new Date(date);
    const today = new Date();
    const daysDiff = Math.ceil((revisionDate.getTime() - today.getTime()) / (1000 * 60 * 60 * 24));
    return daysDiff <= 30 && daysDiff >= 0;
  };

  const isRevisionExpired = (date?: string) => {
    if (!date) return false;
    const revisionDate = new Date(date);
    const today = new Date();
    return revisionDate.getTime() < today.getTime();
  };

  return (
    <Card>
      <Card.Header className="bg-body-tertiary d-flex justify-content-between align-items-center">
        <h5 className="mb-0">
          <FontAwesomeIcon icon="info-circle" className="me-2 text-info" />
          Tachograph Information
        </h5>
        <div>
          {!isEditing ? (
            <Button
              variant="falcon-default"
              size="sm"
              onClick={() => setIsEditing(true)}
            >
              <FontAwesomeIcon icon="edit" className="me-1" />
              Edit
            </Button>
          ) : (
            <>
              <Button
                variant="success"
                size="sm"
                className="me-2"
                onClick={handleSubmit}
                disabled={isUpdating}
              >
                <FontAwesomeIcon icon="save" className="me-1" />
                {isUpdating ? 'Saving...' : 'Save'}
              </Button>
              <Button
                variant="secondary"
                size="sm"
                onClick={handleCancel}
                disabled={isUpdating}
              >
                <FontAwesomeIcon icon="times" className="me-1" />
                Cancel
              </Button>
            </>
          )}
        </div>
      </Card.Header>
      <Card.Body>
        {!isEditing ? (
          <>
            <Row className="mb-3">
              <Col md={6}>
                <small className="text-700 d-block mb-1">Tachograph Name</small>
                <div className="fw-semi-bold">
                  <FontAwesomeIcon icon="gauge-high" className="me-2 text-info" />
                  {tachograph.nome}
                </div>
              </Col>
              <Col md={6}>
                <small className="text-700 d-block mb-1">License Plate</small>
                <div className="fw-semi-bold">
                  <FontAwesomeIcon icon="id-card" className="me-2 text-muted" />
                  {tachograph.targa}
                </div>
              </Col>
            </Row>
            <Row className="mb-3">
              <Col md={6}>
                <small className="text-700 d-block mb-1">Current Location</small>
                <div className="fw-semi-bold">
                  <FontAwesomeIcon icon="map-marker-alt" className="me-2 text-muted" />
                  {tachograph.luogo || 'Not specified'}
                </div>
              </Col>
              <Col md={6}>
                <small className="text-700 d-block mb-1">Status</small>
                <div>
                  <Badge bg={tachograph.isActive ? 'soft-success' : 'soft-secondary'}>
                    <span className={`text-${tachograph.isActive ? 'success' : 'secondary'}`}>
                      {tachograph.isActive ? 'Active' : 'Inactive'}
                    </span>
                  </Badge>
                </div>
              </Col>
            </Row>
            <Row className="mb-3">
              <Col md={6}>
                <small className="text-700 d-block mb-1">Inspection Expiry</small>
                <div className={
                  isRevisionExpired(tachograph.scadenzaRevisione) ? 'fw-semi-bold text-danger' :
                  isRevisionExpiring(tachograph.scadenzaRevisione) ? 'fw-semi-bold text-warning' :
                  'fw-semi-bold'
                }>
                  <FontAwesomeIcon icon="calendar-alt" className="me-2 text-muted" />
                  {formatDate(tachograph.scadenzaRevisione)}
                  {(isRevisionExpiring(tachograph.scadenzaRevisione) || isRevisionExpired(tachograph.scadenzaRevisione)) && (
                    <FontAwesomeIcon icon="exclamation-triangle" className="ms-2" />
                  )}
                </div>
              </Col>
              <Col md={6}>
                <small className="text-700 d-block mb-1">Scheduled Inspection</small>
                <div className="fw-semi-bold">
                  <FontAwesomeIcon icon="calendar-check" className="me-2 text-muted" />
                  {formatDate(tachograph.revisioneProgrammata)}
                </div>
              </Col>
            </Row>

            <hr className="my-3" />

            <Row className="mb-3">
              <Col md={12}>
                <small className="text-700 d-block mb-1">Notes</small>
                <div>{tachograph.note || <span className="text-muted">No notes</span>}</div>
              </Col>
            </Row>
            <hr />
            <Row>
              <Col md={6}>
                <small className="text-muted">
                  Created On: {formatDate(tachograph.createdAt)}
                </small>
              </Col>
              <Col md={6}>
                <small className="text-muted">
                  Last Update: {formatDate(tachograph.updatedAt)}
                </small>
              </Col>
            </Row>
          </>
        ) : (
          <>
            <Row className="g-3">
              <Col md={6}>
                <Form.Group>
                  <Form.Label>Name</Form.Label>
                  <Form.Control
                    type="text"
                    name="nome"
                    value={formData.nome}
                    onChange={handleChange}
                    required
                    maxLength={100}
                  />
                </Form.Group>
              </Col>
              <Col md={6}>
                <Form.Group>
                  <Form.Label>License Plate</Form.Label>
                  <Form.Control
                    type="text"
                    name="targa"
                    value={formData.targa}
                    onChange={handleChange}
                    required
                    maxLength={20}
                    style={{ textTransform: 'uppercase' }}
                  />
                </Form.Group>
              </Col>
              <Col md={12}>
                <Form.Group>
                  <Form.Label>Location</Form.Label>
                  <Form.Control
                    type="text"
                    name="luogo"
                    value={formData.luogo}
                    onChange={handleChange}
                    maxLength={200}
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
                  />
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
                  />
                </Form.Group>
              </Col>
              <Col md={12}>
                <Form.Group>
                  <Form.Label>Notes</Form.Label>
                  <Form.Control
                    as="textarea"
                    rows={3}
                    name="note"
                    value={formData.note}
                    onChange={handleChange}
                    maxLength={500}
                  />
                  <Form.Text className="text-muted">
                    {formData.note.length}/500 characters
                  </Form.Text>
                </Form.Group>
              </Col>
            </Row>
          </>
        )}
      </Card.Body>
    </Card>
  );
};

export default TachographProfileInfo;