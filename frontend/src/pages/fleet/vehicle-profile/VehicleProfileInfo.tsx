import React, { useState } from 'react';
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome';
import { Button, Card, Collapse, Row, Col, Badge, Form } from 'react-bootstrap';
import {
  VehicleResponse,
  useUpdateVehicleMutation
} from 'store/api/vehicleApi';
import { FaTruck, FaTrailer, FaEdit, FaSave, FaTimes } from 'react-icons/fa';

interface VehicleProfileInfoProps {
  vehicle: VehicleResponse;
}

const VehicleProfileInfo: React.FC<VehicleProfileInfoProps> = ({ vehicle }) => {
  const [collapsed, setCollapsed] = useState(false);
  const [isEditing, setIsEditing] = useState(false);
  const [updateVehicle, { isLoading }] = useUpdateVehicleMutation();

  const [formData, setFormData] = useState({
    nome: vehicle.nome,
    targa: vehicle.targa,
    tipo: vehicle.tipo,
    luogo: vehicle.luogo || '',
    note: vehicle.note || '',
    scadenzaRevisione: vehicle.scadenzaRevisione
      ? new Date(vehicle.scadenzaRevisione).toISOString().split('T')[0]
      : '',
    revisioneProgrammata: vehicle.revisioneProgrammata
      ? new Date(vehicle.revisioneProgrammata).toISOString().split('T')[0]
      : '',
    insuranceExpiry: vehicle.insuranceExpiry
      ? new Date(vehicle.insuranceExpiry).toISOString().split('T')[0]
      : '',
    carTaxExpiry: vehicle.carTaxExpiry
      ? new Date(vehicle.carTaxExpiry).toISOString().split('T')[0]
      : ''
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

  // Type labels
  const tipoLabels: Record<string, string> = {
    motrice: 'Truck',
    rimorchio: 'Trailer',
    'semi-rimorchio': 'Semi-trailer',
    trattore: 'Tractor',
    semovente: 'Self-propelled'
  };

  const handleEdit = () => {
    setIsEditing(true);
    setCollapsed(false);
  };

  const handleCancel = () => {
    setIsEditing(false);
    // Reset form data to original values
    setFormData({
      nome: vehicle.nome,
      targa: vehicle.targa,
      tipo: vehicle.tipo,
      luogo: vehicle.luogo || '',
      note: vehicle.note || '',
      scadenzaRevisione: vehicle.scadenzaRevisione
        ? new Date(vehicle.scadenzaRevisione).toISOString().split('T')[0]
        : '',
      revisioneProgrammata: vehicle.revisioneProgrammata
        ? new Date(vehicle.revisioneProgrammata).toISOString().split('T')[0]
        : '',
      insuranceExpiry: vehicle.insuranceExpiry
        ? new Date(vehicle.insuranceExpiry).toISOString().split('T')[0]
        : '',
      carTaxExpiry: vehicle.carTaxExpiry
        ? new Date(vehicle.carTaxExpiry).toISOString().split('T')[0]
        : ''
    });
  };

  const handleSave = async () => {
    try {
      const dataToSubmit: any = {
        nome: formData.nome,
        targa: formData.targa.toUpperCase(),
        tipo: formData.tipo
      };

      // Add optional fields if they have values
      if (formData.luogo) dataToSubmit.luogo = formData.luogo;
      if (formData.note) dataToSubmit.note = formData.note;
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

      await updateVehicle({
        id: vehicle.id,
        data: dataToSubmit
      }).unwrap();

      setIsEditing(false);
    } catch (error) {
      console.error('Failed to update vehicle:', error);
    }
  };

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
  };

  return (
    <Card className="mb-3">
      <Card.Header className="bg-body-tertiary d-flex justify-content-between align-items-center">
        <h5 className="mb-0">Vehicle Information</h5>
        <div>
          {!isEditing ? (
            <Button variant="falcon-default" size="sm" onClick={handleEdit}>
              <FaEdit className="me-1" /> Edit
            </Button>
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

      <Card.Body className="text-1000">
        <Row className="mb-3">
          <Col md={6}>
            <small className="text-700 d-block mb-1">Vehicle Name</small>
            {isEditing ? (
              <Form.Control
                type="text"
                name="nome"
                value={formData.nome}
                onChange={handleChange}
                size="sm"
              />
            ) : (
              <div className="fw-semi-bold">
                {vehicle.tipo === 'motrice' || vehicle.tipo === 'trattore' || vehicle.tipo === 'semovente' ? (
                  <FaTruck className="me-2 text-primary" />
                ) : (
                  <FaTrailer className="me-2 text-primary" />
                )}
                {vehicle.nome}
              </div>
            )}
          </Col>
          <Col md={6}>
            <small className="text-700 d-block mb-1">License Plate</small>
            {isEditing ? (
              <Form.Control
                type="text"
                name="targa"
                value={formData.targa}
                onChange={handleChange}
                size="sm"
              />
            ) : (
              <div className="fw-semi-bold">
                <FontAwesomeIcon icon="id-card" className="me-2 text-muted" />
                {vehicle.targa}
              </div>
            )}
          </Col>
        </Row>

        <Row className="mb-3">
          <Col md={6}>
            <small className="text-700 d-block mb-1">Vehicle Type</small>
            {isEditing ? (
              <Form.Select
                name="tipo"
                value={formData.tipo}
                onChange={handleChange}
                size="sm"
              >
                <option value="motrice">Truck</option>
                <option value="rimorchio">Trailer</option>
                <option value="semi-rimorchio">Semi-trailer</option>
                <option value="trattore">Tractor</option>
                <option value="semovente">Self-propelled</option>
              </Form.Select>
            ) : (
              <Badge bg="soft-primary">
                {tipoLabels[vehicle.tipo] || vehicle.tipo}
              </Badge>
            )}
          </Col>
          <Col md={6}>
            <small className="text-700 d-block mb-1">Status</small>
            <div>
              <Badge bg={vehicle.isActive ? 'soft-success' : 'soft-secondary'}>
                <span
                  className={`text-${vehicle.isActive ? 'success' : 'secondary'}`}
                >
                  {vehicle.isActive ? 'Active' : 'Inactive'}
                </span>
              </Badge>
            </div>
          </Col>
        </Row>

        <Row className="mb-3">
          <Col md={6}>
            <small className="text-700 d-block mb-1">Current Location</small>
            {isEditing ? (
              <Form.Control
                type="text"
                name="luogo"
                value={formData.luogo}
                onChange={handleChange}
                size="sm"
                placeholder="E.g. Calcinaia Depot"
              />
            ) : (
              <div className="fw-semi-bold">
                <FontAwesomeIcon
                  icon="map-marker-alt"
                  className="me-2 text-muted"
                />
                {vehicle.luogo || 'Not specified'}
              </div>
            )}
          </Col>
          <Col md={6}>
            <small className="text-700 d-block mb-1">Created On</small>
            <div>
              <FontAwesomeIcon
                icon="calendar-plus"
                className="me-2 text-muted"
              />
              {formatDate(vehicle.createdAt)}
            </div>
          </Col>
        </Row>

        <hr className="my-3" />

        <h6 className="mb-3">Inspection Information</h6>
        <Row className="mb-3">
          <Col md={6}>
            <small className="text-700 d-block mb-1">Inspection Expiry</small>
            {isEditing ? (
              <Form.Control
                type="date"
                name="scadenzaRevisione"
                value={formData.scadenzaRevisione}
                onChange={handleChange}
                size="sm"
              />
            ) : (
              <div className="fw-semi-bold">
                <FontAwesomeIcon
                  icon="calendar-alt"
                  className="me-2 text-warning"
                />
                {formatDate(vehicle.scadenzaRevisione)}
              </div>
            )}
          </Col>
          <Col md={6}>
            <small className="text-700 d-block mb-1">
              Scheduled Inspection
            </small>
            {isEditing ? (
              <Form.Control
                type="date"
                name="revisioneProgrammata"
                value={formData.revisioneProgrammata}
                onChange={handleChange}
                size="sm"
              />
            ) : (
              <div className="fw-semi-bold">
                <FontAwesomeIcon
                  icon="calendar-check"
                  className="me-2 text-success"
                />
                {formatDate(vehicle.revisioneProgrammata)}
              </div>
            )}
          </Col>
        </Row>

        <hr className="my-3" />

        <h6 className="mb-3">Insurance and Tax Information</h6>
        <Row className="mb-3">
          <Col md={6}>
            <small className="text-700 d-block mb-1">Insurance Expiry</small>
            {isEditing ? (
              <Form.Control
                type="date"
                name="insuranceExpiry"
                value={formData.insuranceExpiry}
                onChange={handleChange}
                size="sm"
              />
            ) : (
              <div className="fw-semi-bold">
                <FontAwesomeIcon
                  icon="shield-alt"
                  className="me-2 text-info"
                />
                {formatDate(vehicle.insuranceExpiry)}
              </div>
            )}
          </Col>
          <Col md={6}>
            <small className="text-700 d-block mb-1">Road Tax Expiry</small>
            {isEditing ? (
              <Form.Control
                type="date"
                name="carTaxExpiry"
                value={formData.carTaxExpiry}
                onChange={handleChange}
                size="sm"
              />
            ) : (
              <div className="fw-semi-bold">
                <FontAwesomeIcon
                  icon="file-invoice-dollar"
                  className="me-2 text-danger"
                />
                {formatDate(vehicle.carTaxExpiry)}
              </div>
            )}
          </Col>
        </Row>

        <Button
          variant="link"
          size="sm"
          onClick={() => setCollapsed(!collapsed)}
          aria-controls="collapse-additional-info"
          aria-expanded={!collapsed}
          className="p-0"
        >
          {collapsed ? 'Show' : 'Hide'} additional details
          <FontAwesomeIcon
            icon="chevron-down"
            transform={collapsed ? 'rotate-180' : ''}
            className="ms-2"
          />
        </Button>

        <Collapse in={!collapsed}>
          <div id="collapse-additional-info">
            <hr className="my-3" />

            <Row className="mb-3">
              <Col xs={12}>
                <small className="text-700 d-block mb-1">Notes</small>
                {isEditing ? (
                  <Form.Control
                    as="textarea"
                    rows={3}
                    name="note"
                    value={formData.note}
                    onChange={handleChange}
                    placeholder="Enter any notes about the vehicle..."
                  />
                ) : (
                  <div className="fw-semi-bold">
                    {vehicle.note || (
                      <span className="text-muted">
                        No notes available
                      </span>
                    )}
                  </div>
                )}
              </Col>
            </Row>

            <Row>
              <Col md={6}>
                <small className="text-700 d-block mb-1">
                  Last Updated
                </small>
                <div className="text-muted">
                  <FontAwesomeIcon icon="clock" className="me-2" />
                  {formatDateTime(vehicle.updatedAt)}
                </div>
              </Col>
              <Col md={6}>
                <small className="text-700 d-block mb-1">Vehicle ID</small>
                <div className="text-muted font-monospace small">
                  {vehicle.id}
                </div>
              </Col>
            </Row>
          </div>
        </Collapse>
      </Card.Body>
    </Card>
  );
};

export default VehicleProfileInfo;
