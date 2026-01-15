import React, { useState } from 'react';
import { Card, Row, Col, Badge, Button, Form } from 'react-bootstrap';
import { User, useUpdateUserMutation } from 'store/api/userApi';
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome';
import { IconProp } from '@fortawesome/fontawesome-svg-core';
import { FaEdit, FaSave, FaTimes } from 'react-icons/fa';

interface AdminLicensesProps {
  user: User;
}

const AdminLicenses: React.FC<AdminLicensesProps> = ({ user }) => {
  const [isEditing, setIsEditing] = useState(false);
  const [updateUser, { isLoading }] = useUpdateUserMutation();

  const [formData, setFormData] = useState({
    licenseNumber: user.licenseNumber || '',
    licenseExpiry: user.licenseExpiry
      ? new Date(user.licenseExpiry).toISOString().split('T')[0]
      : '',
    driverCardNumber: user.driverCardNumber || '',
    driverCardExpiry: user.driverCardExpiry
      ? new Date(user.driverCardExpiry).toISOString().split('T')[0]
      : '',
    cqcExpiry: user.cqcExpiry
      ? new Date(user.cqcExpiry).toISOString().split('T')[0]
      : '',
    adrNumber: user.adrNumber || '',
    adrExpiry: user.adrExpiry
      ? new Date(user.adrExpiry).toISOString().split('T')[0]
      : '',
    tachigrafExpiry: user.tachigrafExpiry
      ? new Date(user.tachigrafExpiry).toISOString().split('T')[0]
      : ''
  });

  // Helper function to format date
  const formatDate = (dateString: string | undefined) => {
    if (!dateString) return 'Not specified';
    return new Date(dateString).toLocaleDateString('en-GB', {
      year: 'numeric',
      month: 'long',
      day: 'numeric'
    });
  };

  // Helper function to check if date is expired
  const isExpired = (dateString: string | undefined) => {
    if (!dateString) return false;
    return new Date(dateString) < new Date();
  };

  // Helper function to check if date is expiring soon (within 30 days)
  const isExpiringSoon = (dateString: string | undefined) => {
    if (!dateString) return false;
    const date = new Date(dateString);
    const now = new Date();
    const thirtyDaysFromNow = new Date();
    thirtyDaysFromNow.setDate(now.getDate() + 30);
    return date > now && date < thirtyDaysFromNow;
  };

  // Helper function to get expiry badge
  const getExpiryBadge = (dateString: string | undefined) => {
    if (!dateString) return null;

    if (isExpired(dateString)) {
      return (
        <Badge bg="danger" className="ms-2">
          Expired
        </Badge>
      );
    }
    if (isExpiringSoon(dateString)) {
      return (
        <Badge bg="warning" className="ms-2">
          Expiring Soon
        </Badge>
      );
    }
    return (
      <Badge bg="success" className="ms-2">
        Valid
      </Badge>
    );
  };

  const handleEdit = () => {
    setIsEditing(true);
  };

  const handleCancel = () => {
    setIsEditing(false);
    // Reset form data to original values
    setFormData({
      licenseNumber: user.licenseNumber || '',
      licenseExpiry: user.licenseExpiry
        ? new Date(user.licenseExpiry).toISOString().split('T')[0]
        : '',
      driverCardNumber: user.driverCardNumber || '',
      driverCardExpiry: user.driverCardExpiry
        ? new Date(user.driverCardExpiry).toISOString().split('T')[0]
        : '',
      cqcExpiry: user.cqcExpiry
        ? new Date(user.cqcExpiry).toISOString().split('T')[0]
        : '',
      adrNumber: user.adrNumber || '',
      adrExpiry: user.adrExpiry
        ? new Date(user.adrExpiry).toISOString().split('T')[0]
        : '',
      tachigrafExpiry: user.tachigrafExpiry
        ? new Date(user.tachigrafExpiry).toISOString().split('T')[0]
        : ''
    });
  };

  const handleSave = async () => {
    try {
      const dataToSubmit: any = {};

      // Add optional fields if they have values
      if (formData.licenseNumber)
        dataToSubmit.licenseNumber = formData.licenseNumber;
      if (formData.licenseExpiry) {
        dataToSubmit.licenseExpiry = new Date(
          formData.licenseExpiry
        ).toISOString();
      }
      if (formData.driverCardNumber)
        dataToSubmit.driverCardNumber = formData.driverCardNumber;
      if (formData.driverCardExpiry) {
        dataToSubmit.driverCardExpiry = new Date(
          formData.driverCardExpiry
        ).toISOString();
      }
      if (formData.cqcExpiry) {
        dataToSubmit.cqcExpiry = new Date(formData.cqcExpiry).toISOString();
      }
      if (formData.adrNumber) dataToSubmit.adrNumber = formData.adrNumber;
      if (formData.adrExpiry) {
        dataToSubmit.adrExpiry = new Date(formData.adrExpiry).toISOString();
      }
      if (formData.tachigrafExpiry) {
        dataToSubmit.tachigrafExpiry = new Date(
          formData.tachigrafExpiry
        ).toISOString();
      }

      await updateUser({
        id: user.id,
        data: dataToSubmit
      }).unwrap();

      setIsEditing(false);
    } catch (error) {
      console.error('Failed to update user certifications:', error);
    }
  };

  const handleChange = (e: React.ChangeEvent<HTMLInputElement>) => {
    const { name, value } = e.target;
    setFormData(prev => ({
      ...prev,
      [name]: value
    }));
  };

  return (
    <Card className="mb-3">
      <Card.Header className="bg-body-tertiary d-flex justify-content-between align-items-center">
        <h5 className="mb-0">
          <FontAwesomeIcon icon={'id-card' as IconProp} className="me-2" />
          Documents and Certifications
        </h5>
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
      <Card.Body>
        <Row className="mb-3 border-bottom pb-x1">
          <Col md={6}>
            <small className="text-700 d-block mb-1">License Number</small>
            {isEditing ? (
              <Form.Control
                type="text"
                name="licenseNumber"
                value={formData.licenseNumber}
                onChange={handleChange}
                size="sm"
                placeholder="e.g. AB1234567"
              />
            ) : (
              <div className="fw-semi-bold">
                <FontAwesomeIcon
                  icon={'id-card' as IconProp}
                  className="me-2 text-muted"
                />
                {user.licenseNumber || 'Not specified'}
              </div>
            )}
          </Col>
          <Col md={6}>
            <small className="text-700 d-block mb-1">License Expiry</small>
            {isEditing ? (
              <Form.Control
                type="date"
                name="licenseExpiry"
                value={formData.licenseExpiry}
                onChange={handleChange}
                size="sm"
              />
            ) : (
              <div className="fw-semi-bold">
                <FontAwesomeIcon
                  icon={'calendar-alt' as IconProp}
                  className="me-2 text-muted"
                />
                {formatDate(user.licenseExpiry)}
                {user.licenseExpiry && getExpiryBadge(user.licenseExpiry)}
              </div>
            )}
          </Col>
        </Row>

        <Row className="mb-3 border-bottom pb-x1">
          <Col md={6}>
            <small className="text-700 d-block mb-1">
              Driver Card Number
            </small>
            {isEditing ? (
              <Form.Control
                type="text"
                name="driverCardNumber"
                value={formData.driverCardNumber}
                onChange={handleChange}
                size="sm"
                placeholder="e.g. ITA123456789"
              />
            ) : (
              <div className="fw-semi-bold">
                <FontAwesomeIcon
                  icon={'id-card' as IconProp}
                  className="me-2 text-muted"
                />
                {user.driverCardNumber || 'Not specified'}
              </div>
            )}
          </Col>
          <Col md={6}>
            <small className="text-700 d-block mb-1">
              Driver Card Expiry
            </small>
            {isEditing ? (
              <Form.Control
                type="date"
                name="driverCardExpiry"
                value={formData.driverCardExpiry}
                onChange={handleChange}
                size="sm"
              />
            ) : (
              <div className="fw-semi-bold">
                <FontAwesomeIcon
                  icon={'calendar-alt' as IconProp}
                  className="me-2 text-muted"
                />
                {formatDate(user.driverCardExpiry)}
                {user.driverCardExpiry &&
                  getExpiryBadge(user.driverCardExpiry)}
              </div>
            )}
          </Col>
        </Row>

        <Row className="mb-3 border-bottom pb-x1">
          <Col md={6}></Col>
          <Col md={6}>
            <small className="text-700 d-block mb-1">CQC Expiry</small>
            {isEditing ? (
              <Form.Control
                type="date"
                name="cqcExpiry"
                value={formData.cqcExpiry}
                onChange={handleChange}
                size="sm"
              />
            ) : (
              <div className="fw-semi-bold">
                <FontAwesomeIcon
                  icon={'award' as IconProp}
                  className="me-2 text-muted"
                />
                {formatDate(user.cqcExpiry)}
                {user.cqcExpiry && getExpiryBadge(user.cqcExpiry)}
              </div>
            )}
          </Col>
        </Row>
        <Row className="mb-3 border-bottom pb-x1">
          <Col md={6}>
            <small className="text-700 d-block mb-1">ADR Number</small>
            {isEditing ? (
              <Form.Control
                type="text"
                name="adrNumber"
                value={formData.adrNumber}
                onChange={handleChange}
                size="sm"
                placeholder="e.g. ADR12345"
              />
            ) : (
              <div className="fw-semi-bold">
                <FontAwesomeIcon
                  icon={'exclamation-triangle' as IconProp}
                  className="me-2 text-muted"
                />
                {user.adrNumber || 'Not specified'}
              </div>
            )}
          </Col>
          <Col md={6}>
            <small className="text-700 d-block mb-1">ADR Expiry</small>
            {isEditing ? (
              <Form.Control
                type="date"
                name="adrExpiry"
                value={formData.adrExpiry}
                onChange={handleChange}
                size="sm"
              />
            ) : (
              <div className="fw-semi-bold">
                <FontAwesomeIcon
                  icon={'calendar-alt' as IconProp}
                  className="me-2 text-muted"
                />
                {formatDate(user.adrExpiry)}
                {user.adrExpiry && getExpiryBadge(user.adrExpiry)}
              </div>
            )}
          </Col>
        </Row>
        <Row>
          <Col md={6}></Col>

          <Col md={6}>
            <small className="text-700 d-block mb-1">
              Tachograph Expiry
            </small>
            {isEditing ? (
              <Form.Control
                type="date"
                name="tachigrafExpiry"
                value={formData.tachigrafExpiry}
                onChange={handleChange}
                size="sm"
              />
            ) : (
              <div className="fw-semi-bold">
                <FontAwesomeIcon
                  icon={'gauge-high' as IconProp}
                  className="me-2 text-muted"
                />
                {formatDate(user.tachigrafExpiry)}
                {user.tachigrafExpiry && getExpiryBadge(user.tachigrafExpiry)}
              </div>
            )}
          </Col>
        </Row>
      </Card.Body>
    </Card>
  );
};

export default AdminLicenses;
